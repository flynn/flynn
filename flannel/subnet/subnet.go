package subnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	log "github.com/golang/glog"

	"github.com/flynn/flynn/flannel/pkg/ip"
	"github.com/flynn/flynn/flannel/pkg/task"
)

const (
	registerRetries = 10
	subnetTTL       = 24 * 3600
	renewMargin     = time.Hour
)

const (
	SubnetAdded = iota
	SubnetRemoved
)

var (
	subnetRegex *regexp.Regexp = regexp.MustCompile(`(\d+\.\d+.\d+.\d+)-(\d+)`)
)

type LeaseAttrs struct {
	PublicIP    ip.IP4
	HTTPPort    string
	BackendType string          `json:",omitempty"`
	BackendData json.RawMessage `json:",omitempty"`
}

type SubnetLease struct {
	Network ip.IP4Net
	Attrs   LeaseAttrs
}

type SubnetManager struct {
	registry  Registry
	config    *Config
	leaseExp  time.Time
	lastIndex uint64

	mtx     sync.RWMutex
	myLease SubnetLease
	leases  []SubnetLease
}

type EventType int

type Event struct {
	Type  EventType
	Lease SubnetLease
}

type EventBatch []Event

func NewSubnetManager(r Registry) (*SubnetManager, error) {
	config, err := r.GetConfig()
	if err != nil {
		return nil, err
	}

	c, err := ParseConfig(config)
	if err != nil {
		return nil, err
	}

	sm := SubnetManager{
		registry: r,
		config:   c,
	}

	return &sm, nil
}

func (sm *SubnetManager) Lease() SubnetLease {
	sm.mtx.RLock()
	defer sm.mtx.RUnlock()
	return sm.myLease
}

func (sm *SubnetManager) Leases() []SubnetLease {
	sm.mtx.RLock()
	defer sm.mtx.RUnlock()
	res := make([]SubnetLease, len(sm.leases))
	copy(res, sm.leases)
	return res
}

func (sm *SubnetManager) AcquireLease(attrs *LeaseAttrs, cancel chan bool) (ip.IP4Net, error) {
	for {
		sn, err := sm.acquireLeaseOnce(attrs, cancel)
		switch {
		case err == nil:
			log.Info("Subnet lease acquired: ", sn)
			return sn, nil

		case err == task.ErrCanceled:
			return ip.IP4Net{}, err

		default:
			log.Error("Failed to acquire subnet: ", err)
		}

		select {
		case <-time.After(time.Second):

		case <-cancel:
			return ip.IP4Net{}, task.ErrCanceled
		}
	}
}

func findLeaseByIP(leases []SubnetLease, pubIP ip.IP4) *SubnetLease {
	for _, l := range leases {
		if pubIP == l.Attrs.PublicIP {
			return &l
		}
	}

	return nil
}

func (sm *SubnetManager) tryAcquireLease(extIP ip.IP4, attrs *LeaseAttrs) (ip.IP4Net, error) {
	sm.mtx.Lock()
	defer sm.mtx.Unlock()

	var err error
	sm.leases, err = sm.getLeases()
	if err != nil {
		return ip.IP4Net{}, err
	}

	attrBytes, err := json.Marshal(attrs)
	if err != nil {
		log.Errorf("marshal failed: %#v, %v", attrs, err)
		return ip.IP4Net{}, err
	}

	// try to reuse a subnet if there's one that matches our IP
	if l := findLeaseByIP(sm.leases, extIP); l != nil {
		resp, err := sm.registry.UpdateSubnet(l.Network.StringSep(".", "-"), string(attrBytes), subnetTTL)
		if err != nil {
			return ip.IP4Net{}, err
		}

		sm.myLease.Network = l.Network
		sm.myLease.Attrs = *attrs
		sm.leaseExp = *resp.Expiration
		return l.Network, nil
	}

	// no existing match, grab a new one
	sn, err := sm.allocateSubnet()
	if err != nil {
		return ip.IP4Net{}, err
	}

	resp, err := sm.registry.CreateSubnet(sn.StringSep(".", "-"), string(attrBytes), subnetTTL)
	switch {
	case err == nil:
		sm.myLease.Network = sn
		sm.myLease.Attrs = *attrs
		sm.leaseExp = *resp.Expiration
		return sn, nil

	case err == ErrSubnetExists:
		// if subnet already exists, try again.
		return ip.IP4Net{}, nil

	default:
		return ip.IP4Net{}, err
	}
}

func (sm *SubnetManager) acquireLeaseOnce(attrs *LeaseAttrs, cancel chan bool) (ip.IP4Net, error) {
	for i := 0; i < registerRetries; i++ {
		sn, err := sm.tryAcquireLease(attrs.PublicIP, attrs)
		switch {
		case err != nil:
			return ip.IP4Net{}, err
		case sn.IP != 0:
			return sn, nil
		}

		// before moving on, check for cancel
		if interrupted(cancel) {
			return ip.IP4Net{}, task.ErrCanceled
		}
	}

	return ip.IP4Net{}, errors.New("Max retries reached trying to acquire a subnet")
}

func (sm *SubnetManager) GetConfig() *Config {
	return sm.config
}

/// Implementation
func parseSubnetKey(s string) (ip.IP4Net, error) {
	if parts := subnetRegex.FindStringSubmatch(s); len(parts) == 3 {
		snIp := net.ParseIP(parts[1]).To4()
		prefixLen, err := strconv.ParseUint(parts[2], 10, 5)
		if snIp != nil && err == nil {
			return ip.IP4Net{IP: ip.FromIP(snIp), PrefixLen: uint(prefixLen)}, nil
		}
	}

	return ip.IP4Net{}, errors.New("Error parsing IP Subnet")
}

func (sm *SubnetManager) getLeases() ([]SubnetLease, error) {
	resp, err := sm.registry.GetSubnets()
	if err != nil {
		return nil, err
	}
	var leases []SubnetLease
	for subnet, rawLeaseAttrs := range resp.Subnets {
		sn, err := parseSubnetKey(subnet)
		if err == nil {
			var attrs LeaseAttrs
			if err = json.Unmarshal(rawLeaseAttrs, &attrs); err == nil {
				lease := SubnetLease{sn, attrs}
				leases = append(leases, lease)
			}
		}
	}
	sm.lastIndex = resp.Index
	return leases, nil
}

func deleteLease(l []SubnetLease, i int) []SubnetLease {
	l[i], l = l[len(l)-1], l[:len(l)-1]
	return l
}

func (sm *SubnetManager) applySubnetChange(action string, ipn ip.IP4Net, data []byte) (Event, error) {
	sm.mtx.Lock()
	defer sm.mtx.Unlock()

	switch action {
	case "delete", "expire":
		for i, l := range sm.leases {
			if l.Network.Equal(ipn) {
				deleteLease(sm.leases, i)
				return Event{SubnetRemoved, l}, nil
			}
		}

		log.Errorf("Removed subnet (%s) was not found", ipn)
		return Event{
			SubnetRemoved,
			SubnetLease{ipn, LeaseAttrs{}},
		}, nil

	default:
		var attrs LeaseAttrs
		err := json.Unmarshal(data, &attrs)
		if err != nil {
			return Event{}, err
		}

		for i, l := range sm.leases {
			if l.Network.Equal(ipn) {
				sm.leases[i] = SubnetLease{ipn, attrs}
				return Event{SubnetAdded, sm.leases[i]}, nil
			}
		}

		sm.leases = append(sm.leases, SubnetLease{ipn, attrs})
		return Event{SubnetAdded, sm.leases[len(sm.leases)-1]}, nil
	}
}

func (sm *SubnetManager) allocateSubnet() (ip.IP4Net, error) {
	log.Infof("Picking subnet in range %s ... %s", sm.config.SubnetMin, sm.config.SubnetMax)

	var bag []ip.IP4
	sn := ip.IP4Net{IP: sm.config.SubnetMin, PrefixLen: sm.config.SubnetLen}

OuterLoop:
	for ; sn.IP <= sm.config.SubnetMax && len(bag) < 100; sn = sn.Next() {
		for _, l := range sm.leases {
			if sn.Overlaps(l.Network) {
				continue OuterLoop
			}
		}
		bag = append(bag, sn.IP)
	}

	if len(bag) == 0 {
		return ip.IP4Net{}, errors.New("out of subnets")
	} else {
		i := randInt(0, len(bag))
		return ip.IP4Net{IP: bag[i], PrefixLen: sm.config.SubnetLen}, nil
	}
}

func (sm *SubnetManager) WatchLeases(receiver chan EventBatch, cancel chan bool) {
	// "catch up" by replaying all the leases we discovered during
	// AcquireLease
	var batch EventBatch
	sm.mtx.RLock()
	for _, l := range sm.leases {
		if !sm.myLease.Network.Equal(l.Network) {
			batch = append(batch, Event{SubnetAdded, l})
		}
	}
	sm.mtx.RUnlock()
	if len(batch) > 0 {
		receiver <- batch
	}

	for {
		resp, err := sm.registry.WatchSubnets(sm.lastIndex+1, cancel)

		// WatchSubnets exited by cancel chan being signaled
		if err == nil && resp == nil {
			return
		}

		var batch *EventBatch
		if err == nil {
			batch, err = sm.parseSubnetWatchResponse(resp)
		}

		if err != nil {
			log.Errorf("%v", err)
			time.Sleep(time.Second)
			continue
		}

		if batch != nil {
			receiver <- *batch
		}
	}
}

func (sm *SubnetManager) parseSubnetWatchResponse(resp *Response) (batch *EventBatch, err error) {
	sm.lastIndex = resp.Index

	for subnet, rawLeaseAttrs := range resp.Subnets {
		sn, err := parseSubnetKey(subnet)
		if err != nil {
			return nil, fmt.Errorf("Error parsing subnet IP: %s", subnet)
		}

		// Don't process our own changes
		if !sm.myLease.Network.Equal(sn) {
			evt, err := sm.applySubnetChange(resp.Action, sn, rawLeaseAttrs)
			if err != nil {
				return nil, err
			}
			batch = &EventBatch{evt}
		}
	}

	return
}

func (sm *SubnetManager) LeaseRenewer(cancel chan bool) {
	for {
		dur := sm.leaseExp.Sub(time.Now()) - renewMargin

		select {
		case <-time.After(dur):
			sm.mtx.RLock()
			attrBytes, err := json.Marshal(&sm.myLease.Attrs)
			sm.mtx.RUnlock()
			if err != nil {
				log.Error("Error renewing lease (trying again in 1 min): ", err)
				dur = time.Minute
				continue
			}

			sm.mtx.RLock()
			resp, err := sm.registry.UpdateSubnet(sm.myLease.Network.StringSep(".", "-"), string(attrBytes), subnetTTL)
			sm.mtx.RUnlock()
			if err != nil {
				log.Error("Error renewing lease (trying again in 1 min): ", err)
				dur = time.Minute
				continue
			}

			sm.leaseExp = *resp.Expiration
			log.Info("Lease renewed, new expiration: ", sm.leaseExp)
		case <-cancel:
			return
		}
	}
}

func interrupted(cancel chan bool) bool {
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}
