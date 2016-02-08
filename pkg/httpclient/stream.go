package httpclient

import (
	"bufio"
	"io"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/stream"
)

/*
	Stream manufactures a `pkg/stream.Stream`, starts a worker pumping events out of decoding, and returns that.

	The 'outputCh' parameter must be a sendable channel.  The "zero"-values of channel's content type will be created and used in the deserialization, then sent.

	The return values from `httpclient.RawReq` are probably a useful starting point for the 'res' parameter.

	Closing the returned `stream.Stream` shuts down the worker.
*/
func Stream(res *http.Response, outputCh interface{}) stream.Stream {
	stream := stream.New()

	var chanValue reflect.Value
	if v, ok := outputCh.(reflect.Value); ok {
		chanValue = v
	} else {
		chanValue = reflect.ValueOf(outputCh)
	}

	stopChanValue := reflect.ValueOf(stream.StopCh)
	msgType := chanValue.Type().Elem().Elem()
	go func() {
		done := make(chan struct{})
		defer func() {
			chanValue.Close()
			close(done)
		}()

		go func() {
			select {
			case <-stream.StopCh:
			case <-done:
			}
			res.Body.Close()
		}()

		r := bufio.NewReader(res.Body)
		dec := sse.NewDecoder(r)
		for {
			msg := reflect.New(msgType)
			if err := dec.Decode(msg.Interface()); err != nil {
				if err != io.EOF {
					stream.Error = err
				}
				break
			}
			chosen, _, _ := reflect.Select([]reflect.SelectCase{
				{
					Dir:  reflect.SelectRecv,
					Chan: stopChanValue,
				},
				{
					Dir:  reflect.SelectSend,
					Chan: chanValue,
					Send: msg,
				},
			})
			switch chosen {
			case 0:
				return
			default:
			}
		}
	}()
	return stream
}

var connectAttempts = attempt.Strategy{
	Total: 20 * time.Second,
	Delay: 100 * time.Millisecond,
}

func ResumingStream(connect func(int64) (*http.Response, error, bool), outputCh interface{}) (stream.Stream, error) {
	stream := stream.New()
	firstErr := make(chan error)
	go func() {
		var once sync.Once
		var lastID int64
		stopChanValue := reflect.ValueOf(stream.StopCh)
		outValue := reflect.ValueOf(outputCh)
		defer outValue.Close()
		for {
			var res *http.Response
			// nonRetryableErr will be set if a connection attempt should not
			// be retried (for example if a 404 is returned).
			var nonRetryableErr error
			err := connectAttempts.Run(func() (err error) {
				var retry bool
				res, err, retry = connect(lastID)
				if !retry {
					nonRetryableErr = err
					return nil
				}
				return
			})
			if nonRetryableErr != nil {
				err = nonRetryableErr
			}
			once.Do(func() { firstErr <- err })
			if err != nil {
				stream.Error = err
				return
			}
			chanValue := reflect.MakeChan(outValue.Type(), 0)
			s := Stream(res, chanValue)
		loop:
			for {
				chosen, v, ok := reflect.Select([]reflect.SelectCase{
					{
						Dir:  reflect.SelectRecv,
						Chan: stopChanValue,
					},
					{
						Dir:  reflect.SelectRecv,
						Chan: chanValue,
					},
				})
				switch chosen {
				case 0:
					s.Close()
					return
				default:
					if !ok {
						// TODO: check s.Err() for a special error sent from the
						//       server indicating the stream should not be retried
						break loop
					}
					id := v.Elem().FieldByName("ID")
					if id.Kind() == reflect.Int64 {
						lastID = id.Int()
					}
					outValue.Send(v)
				}
			}
		}
	}()
	return stream, <-firstErr
}
