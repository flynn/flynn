package subnet

import (
	"errors"
	"time"
)

var (
	ErrSubnetExists = errors.New("subnet exists")
)

type Response struct {
	Subnets    map[string][]byte
	Index      uint64
	Expiration *time.Time
	Action     string
}

type Registry interface {
	GetConfig() ([]byte, error)
	GetSubnets() (*Response, error)
	CreateSubnet(sn, data string, ttl uint64) (*Response, error)
	UpdateSubnet(sn, data string, ttl uint64) (*Response, error)
	WatchSubnets(since uint64, stop chan bool) (*Response, error)
}
