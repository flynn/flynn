/*
NotifyDispatcher attempts to make working with a single Listener easy for a
dynamic set of independent listeners.


Usage

Viz:

    import (
        "github.com/lib/pq"
        "github.com/johto/notifyutils/notifydispatcher"
        "fmt"
        "time"
    )

    func listener(dispatcher *notifydispatcher.NotifyDispatcher) {
        ch := make(chan *pq.Notification, 8)
        err := dispatcher.Listen("listenerchannel", ch)
        if err != nil {
            panic(err)
        }
        for n := range ch {
            if n == nil {
                fmt.Println("lost connection, but we're fine now!")
                continue
            }

            fmt.Println("received notification!")
            // do something with notification
        }
        panic("could not keep up!")
    }

    func main() {
        dispatcher := notifydispatcher.NewNotifyDispatcher(pq.NewListener("", time.Second, time.Minute, nil))
        for i := 0; i < 8; i++ {
            go listener(dispatcher)
        }
        select{}
    }

*/
package notifydispatcher

import (
	"container/list"
	"errors"
	"fmt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"sync"
)

var (
	ErrChannelAlreadyActive = errors.New("channel is already active")
	ErrChannelNotActive     = errors.New("channel is not active")
)

var errClosed = errors.New("NotifyDispatcher has been closed")

// SlowReaderEliminationStrategy controls the behaviour of the dispatcher in
// case the buffer of a listener's channel is full and attempting to send to it
// would block the dispatcher, preventing it from delivering notifications for
// unrelated listeners.  The default is CloseSlowReaders, but it can be changed
// at any point during a dispatcher's lifespan using
// SetSlowReaderEliminationStrategy.
type SlowReaderEliminationStrategy int

const (
	// When a send would block, the listener's channel is removed from the set
	// of listeners for that notification channel, and the channel is closed.
	// This is the default strategy.
	CloseSlowReaders SlowReaderEliminationStrategy = iota

	// When a send would block, the notification is not delivered.  Delivery is
	// not reattempted.
	NeglectSlowReaders
)

type listenRequest struct {
	channel  string
	unlisten bool
}

type BroadcastChannel struct {
	Channel chan struct{}
	elem    *list.Element
}

// This is the part of *pq.Listener's interface we're using.  pq itself doesn't
// provide such an interface, but we can just roll our own.
type Listener interface {
	Listen(channel string) error
	Unlisten(channel string) error
	NotificationChannel() <-chan *pq.Notification
}

type NotifyDispatcher struct {
	listener Listener

	// Some details about the behaviour.  Only touch or look at while holding
	// "lock".
	slowReaderEliminationStrategy SlowReaderEliminationStrategy
	broadcastOnConnectionLoss     bool
	broadcastChannels             *list.List

	listenRequestch chan listenRequest

	lock     sync.Mutex
	channels map[string]*listenSet
	closed   bool
	// provide an escape hatch for goroutines sending on listenRequestch
	closeChannel chan bool
}

// NewNotifyDispatcher creates a new NotifyDispatcher, using the supplied
// pq.Listener underneath.  The ownership of the Listener is transferred to
// NotifyDispatcher.  You should not use it after calling NewNotifyDispatcher.
func NewNotifyDispatcher(l Listener) *NotifyDispatcher {
	d := &NotifyDispatcher{
		listener:                      l,
		slowReaderEliminationStrategy: CloseSlowReaders,
		broadcastOnConnectionLoss:     true,
		broadcastChannels:             list.New(),
		listenRequestch:               make(chan listenRequest, 64),
		channels:                      make(map[string]*listenSet),
		closeChannel:                  make(chan bool),
	}
	go d.dispatcherLoop()
	go d.listenRequestHandlerLoop()
	return d
}

// Sets the strategy for mitigating the adverse effects slow readers might have
// on the dispatcher.  See SlowReaderEliminationStrategy.
func (d *NotifyDispatcher) SetSlowReaderEliminationStrategy(strategy SlowReaderEliminationStrategy) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.slowReaderEliminationStrategy = strategy
}

// Controls whether a nil notification from the underlying Listener is
// broadcast to all channels in the set.
func (d *NotifyDispatcher) SetBroadcastOnConnectionLoss(value bool) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.broadcastOnConnectionLoss = value
}

// Opens a new "broadcast channel".  A broadcast channel is sent to by the
// NotifyDispatcher every time the underlying Listener has re-established its
// database connection.
func (d *NotifyDispatcher) OpenBroadcastChannel() BroadcastChannel {
	d.lock.Lock()
	defer d.lock.Unlock()

	ch := BroadcastChannel{
		Channel: make(chan struct{}, 1),
	}
	ch.elem = d.broadcastChannels.PushFront(ch)
	return ch
}

// Closes the broadcast channel ch.
func (d *NotifyDispatcher) CloseBroadcastChannel(ch BroadcastChannel) {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.broadcastChannels.Remove(ch.elem) != ch {
		panic("oops")
	}
	close(ch.Channel)
}

// Broadcast on all broadcastChannels and on all channels unless
// broadcastOnConnectionLoss is disabled.
func (d *NotifyDispatcher) broadcast() {
	d.lock.Lock()
	for e := d.broadcastChannels.Front(); e != nil; e = e.Next() {
		select {
		case e.Value.(BroadcastChannel).Channel <- struct{}{}:
		default:
			// nothing to do
		}
	}

	if !d.broadcastOnConnectionLoss {
		d.lock.Unlock()
		return
	}

	var reapchans []string
	for channel, set := range d.channels {
		if !set.broadcast(d.slowReaderEliminationStrategy, nil) {
			reapchans = append(reapchans, channel)
		}
	}
	d.lock.Unlock()

	for _, ch := range reapchans {
		d.listenRequestch <- listenRequest{ch, true}
	}
}

func (d *NotifyDispatcher) dispatch(n *pq.Notification) {
	reap := false

	d.lock.Lock()
	set, ok := d.channels[n.Channel]
	if ok {
		reap = !set.broadcast(d.slowReaderEliminationStrategy, n)
	}
	d.lock.Unlock()

	if reap {
		d.listenRequestch <- listenRequest{n.Channel, true}
	}
}

func (d *NotifyDispatcher) dispatcherLoop() {
	notifyCh := d.listener.NotificationChannel()

dispatcherLoop:
	for {
		var n *pq.Notification
		select {
		case <-d.closeChannel:
			break dispatcherLoop
		case n = <-notifyCh:
		}

		if n == nil {
			d.broadcast()
		} else {
			d.dispatch(n)
		}
	}
}

// Attempt to start listening on channel.  The caller should not be holding
// lock.
func (d *NotifyDispatcher) execListen(channel string) {
	for {
		err := d.listener.Listen(channel)
		// ErrChannelAlreadyOpen is a valid return value here; we could have
		// abandoned a channel in Unlisten() if the server returned an error
		// for no apparent reason.
		if err == nil ||
			err == pq.ErrChannelAlreadyOpen {
			break
		}
	}

	d.lock.Lock()
	defer d.lock.Unlock()
	set, ok := d.channels[channel]
	if !ok {
		panic("oops")
	}
	set.markActive()
}

func (d *NotifyDispatcher) execUnlisten(channel string) {
	// we don't really care about the error
	_ = d.listener.Unlisten(channel)

	d.lock.Lock()
	set, ok := d.channels[channel]
	if !ok {
		panic("oops")
	}
	if set.state != listenSetStateZombie {
		panic("oops")
	}
	if set.reap() {
		delete(d.channels, channel)
		d.lock.Unlock()
	} else {
		// Couldn't reap the set because it got new listeners while we were
		// waiting for the UNLISTEN to go through.  Re-LISTEN it, but remember
		// to release the lock first.
		d.lock.Unlock()

		d.execListen(channel)
	}
}

func (d *NotifyDispatcher) listenRequestHandlerLoop() {
	for {
		// check closeChannel, just in case we've been closed and there's a
		// backlog of requests
		select {
		case <-d.closeChannel:
			return
		default:
		}

		select {
		case <-d.closeChannel:
			return

		case req := <-d.listenRequestch:
			if req.unlisten {
				d.execUnlisten(req.channel)
			} else {
				d.execListen(req.channel)
			}
		}
	}
}

func (d *NotifyDispatcher) requestListen(channel string, unlisten bool) error {
	select {
	// make sure we don't get stuck here if someone Close()s us
	case <-d.closeChannel:
		return errClosed
	case d.listenRequestch <- listenRequest{channel, unlisten}:
		return nil
	}

	panic("not reached")
}

// Listen adds ch to the set of listeners for notification channel channel.  ch
// should be a buffered channel.  If ch is already in the set of listeners for
// channel, ErrChannelAlreadyActive is returned.  After Listen has returned,
// the notification channel is open and the dispatcher will attempt to deliver
// all notifications received for that channel to ch.
//
// If SlowReaderEliminationStrategy is CloseSlowReaders, reusing the same ch
// for multiple notification channels is not allowed.
func (d *NotifyDispatcher) Listen(channel string, ch chan<- *pq.Notification) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.closed {
		return errClosed
	}

	set, ok := d.channels[channel]
	if ok {
		err := set.add(ch)
		if err != nil {
			return err
		}
	} else {
		set = d.newListenSet(ch)
		d.channels[channel] = set

		// must not be holding the lock while requesting a listen
		d.lock.Unlock()
		err := d.requestListen(channel, false)
		if err != nil {
			return err
		}
		d.lock.Lock()
	}

	return set.waitForActive(&d.closed)
}

// Removes ch from the set of listeners for notification channel channel.  If
// ch is not in the set of listeners for channel, ErrChannelNotActive is
// returned.
func (d *NotifyDispatcher) Unlisten(channel string, ch chan<- *pq.Notification) error {
	d.lock.Lock()

	if d.closed {
		d.lock.Unlock()
		return errClosed
	}

	set, ok := d.channels[channel]
	if !ok {
		d.lock.Unlock()
		return ErrChannelNotActive
	}
	last, err := set.remove(ch)
	d.lock.Unlock()
	if err != nil {
		return err
	}
	if !last {
		// the set isn't empty; nothing further for us to do
		return nil
	}

	// we were the last listener, cue the reaper
	return d.requestListen(channel, true)
}

func (d *NotifyDispatcher) Close() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.closed {
		return errClosed
	}

	d.closed = true
	close(d.closeChannel)

	for _, set := range d.channels {
		set.activeOrClosedCond.Broadcast()
	}

	return nil
}

type listenSetState int

const (
	// The set was recently spawned or respawned, and it's waiting for a call
	// to Listen() to succeed.
	listenSetStateNewborn listenSetState = iota
	// The set is ready and any notifications from the database will be
	// dispatched to the set.
	listenSetStateActive
	// The set has recently been emptied, and it's waiting for a call to
	// Unlisten() to finish.
	listenSetStateZombie
)

type listenSet struct {
	channels           map[chan<- *pq.Notification]struct{}
	state              listenSetState
	activeOrClosedCond *sync.Cond
}

func (d *NotifyDispatcher) newListenSet(firstInhabitant chan<- *pq.Notification) *listenSet {
	s := &listenSet{
		channels: make(map[chan<- *pq.Notification]struct{}),
		state:    listenSetStateNewborn,
	}
	s.activeOrClosedCond = sync.NewCond(&d.lock)
	s.channels[firstInhabitant] = struct{}{}
	return s
}

func (s *listenSet) setState(newState listenSetState) {
	var expectedState listenSetState
	switch newState {
	case listenSetStateNewborn:
		expectedState = listenSetStateZombie
	case listenSetStateActive:
		expectedState = listenSetStateNewborn
	case listenSetStateZombie:
		expectedState = listenSetStateActive
	}
	if s.state != expectedState {
		panic(fmt.Sprintf("illegal state transition from %v to %v", s.state, newState))
	}
	s.state = newState
	if s.state == listenSetStateActive {
		s.activeOrClosedCond.Broadcast()
	}
}

func (s *listenSet) add(ch chan<- *pq.Notification) error {
	_, ok := s.channels[ch]
	if ok {
		return ErrChannelAlreadyActive
	}
	s.channels[ch] = struct{}{}
	return nil
}

// Removes ch from the listen set.  last is false if ch was the last listener
// in the set and the caller should request an UNLISTEN.
func (s *listenSet) remove(ch chan<- *pq.Notification) (last bool, err error) {
	_, ok := s.channels[ch]
	if !ok {
		return false, ErrChannelNotActive
	}
	delete(s.channels, ch)

	if len(s.channels) == 0 {
		s.setState(listenSetStateZombie)
		return true, nil
	}
	return false, nil
}

// Sends n to all listeners in the set, using the supplied
// SlowReaderEliminationStrategy.  n may be nil.  Returns false if the set is
// empty (or was emptied as a result of eliminating slow readers) and the
// caller should request an UNLISTEN on it.  The caller should be holding
// d.lock.
func (s *listenSet) broadcast(strategy SlowReaderEliminationStrategy, n *pq.Notification) bool {
	// must be active
	if s.state != listenSetStateActive {
		return true
	}

	for ch := range s.channels {
		select {
		case ch <- n:

		default:
			if strategy == CloseSlowReaders {
				delete(s.channels, ch)
				close(ch)
			}
		}
	}

	if len(s.channels) == 0 {
		s.setState(listenSetStateZombie)
		return false
	}

	return true
}

// Marks the set active after a successful call to Listen().
func (s *listenSet) markActive() {
	s.setState(listenSetStateActive)
}

// Wait for the listen set to become "active".  Returns nil if successfull, or
// errClosed if the dispatcher was closed while waiting.  The caller should be
// holding d.lock.
func (s *listenSet) waitForActive(closed *bool) error {
	for {
		if *closed {
			return errClosed
		}
		if s.state == listenSetStateActive {
			return nil
		}
		s.activeOrClosedCond.Wait()
	}
}

// Try to reap a zombie set after Unlisten().  Returns true if the set should
// be removed, false otherwise.
func (s *listenSet) reap() bool {
	if s.state != listenSetStateZombie {
		panic("unexpected state in reap")
	}

	if len(s.channels) > 0 {
		// we need to be respawned
		s.setState(listenSetStateNewborn)
		return false
	}

	return true
}
