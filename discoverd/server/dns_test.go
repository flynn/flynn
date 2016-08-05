package server

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	. "github.com/flynn/go-check"
	"github.com/miekg/dns"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type DNSSuite struct{}

var _ = Suite(&DNSSuite{})

func (s *DNSSuite) newServer(c *C, recursors []string) *DNSServer {
	srv := &DNSServer{
		UDPAddr:   "127.0.0.1:0",
		TCPAddr:   "127.0.0.1:0",
		Recursors: recursors,
	}
	srv.SetStore(&DNSServerStore{
		InstancesFn:     func(service string) ([]*discoverd.Instance, error) { return nil, nil },
		ServiceLeaderFn: func(service string) (*discoverd.Instance, error) { return nil, nil },
	})
	c.Assert(srv.ListenAndServe(), IsNil)
	return srv
}

func (s *DNSSuite) TestRecursorParsing(c *C) {
	srv := s.newServer(c, []string{
		"2001:4860:4860::8844",
		"[2001:4860:4860::8888]:553",
		"8.8.8.8",
		"8.8.4.4:5553",
		"google-public-dns-a.google.com",
		"google-public-dns-b.google.com:55553",
	})
	srv.Close()
	c.Assert(srv.Recursors, DeepEquals, []string{
		"[2001:4860:4860::8844]:53",
		"[2001:4860:4860::8888]:553",
		"8.8.8.8:53",
		"8.8.4.4:5553",
		"8.8.8.8:53",
		"8.8.4.4:55553",
	})
}

func startUpstreamTestServer(c *C) (string, string, func()) {
	h := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		res := &dns.Msg{}
		res.SetReply(req)
		switch req.Question[0].Name {
		case "long-compressed-response.":
			for i := 0; i < 25; i++ {
				res.Answer = append(res.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   req.Question[0].Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
					},
					A: net.IP{192, 168, 0, byte(i)},
				})
			}
			if res.Len() <= 512 {
				panic("not huge")
			}
			res.Compress = true
			if res.Len() > 512 {
				panic("too big compressed")
			}
			w.WriteMsg(res)
		}
	})
	up := make(chan struct{}, 2)
	notifyStart := func() { up <- struct{}{} }

	udpListener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	c.Assert(err, IsNil)
	udp := &dns.Server{
		Net:               "udp",
		PacketConn:        udpListener,
		Handler:           h,
		NotifyStartedFunc: notifyStart,
	}
	go udp.ActivateAndServe()

	tcpListener, err := net.Listen("tcp4", "127.0.0.1:0")
	c.Assert(err, IsNil)
	tcp := &dns.Server{
		Net:               "tcp",
		Listener:          tcpListener,
		Handler:           h,
		NotifyStartedFunc: notifyStart,
	}
	go tcp.ActivateAndServe()

	for i := 0; i < 2; i++ {
		select {
		case <-up:
		case <-time.After(5 * time.Second):
			c.Fatal("timed out waiting for server to start")
		}
	}

	return udpListener.(*net.UDPConn).LocalAddr().String(), tcpListener.Addr().String(), func() {
		udp.Shutdown()
		tcp.Shutdown()
	}
}

func (s *DNSSuite) TestRecursor(c *C) {
	udpAddr, tcpAddr, cleanup := startUpstreamTestServer(c)
	defer cleanup()

	withResolvers := func(resolvers []string, net string, f func(string)) {
		srv := s.newServer(c, resolvers)
		defer func() { c.Assert(srv.Close(), IsNil) }()
		addr := srv.UDPAddr
		if net == "tcp" {
			addr = srv.TCPAddr
		}
		f(addr)
	}

	for net, upstreamAddr := range map[string]string{"tcp": tcpAddr, "udp": udpAddr} {
		c.Log(net)
		client := &dns.Client{
			ReadTimeout: 10 * time.Second,
			Net:         net,
		}
		msg := &dns.Msg{}
		msg.SetQuestion("long-compressed-response.", dns.TypeA)

		// Valid request
		withResolvers([]string{upstreamAddr}, net, func(addr string) {
			res, _, err := client.Exchange(msg, addr)
			c.Assert(err, IsNil)
			c.Assert(res.Rcode, Equals, dns.RcodeSuccess)
			c.Assert(len(res.Answer) > 0, Equals, true)
		})

		// Failing recursor fallback
		withResolvers([]string{"127.1.1.1:55", upstreamAddr}, net, func(addr string) {
			res, _, err := client.Exchange(msg, addr)
			c.Assert(err, IsNil)
			c.Assert(res.Rcode, Equals, dns.RcodeSuccess)
			c.Assert(len(res.Answer) > 0, Equals, true)
		})

		// All failing
		withResolvers([]string{"127.1.1.1:55"}, net, func(addr string) {
			res, _, err := client.Exchange(msg, addr)
			c.Assert(err, IsNil)
			c.Assert(res.Rcode, Equals, dns.RcodeServerFailure)
		})
	}
}

func (s *DNSSuite) TestServiceLookup(c *C) {
	type test struct {
		name   string
		domain string
		qs     map[uint16][]testAddr
		data   []*discoverd.Instance
		net    string
	}

	simpleData := make([]*discoverd.Instance, 3)
	simpleAddrs := make([]testAddr, 3)
	simpleData[0], simpleAddrs[0] = fakeStaticInstance("tcp", "192.168.0.1", 80)
	simpleData[1], simpleAddrs[1] = fakeStaticInstance("tcp", "192.168.0.2", 81)
	simpleData[2], simpleAddrs[2] = fakeStaticInstance("tcp", "192.168.0.3", 82)

	mixedData := make([]*discoverd.Instance, 3)
	mixedAddrs := make([]testAddr, 3)
	copy(mixedData, simpleData)
	copy(mixedAddrs, simpleAddrs)
	mixedData[0], mixedAddrs[0] = fakeStaticInstance("udp", "192.168.0.5", 99)
	filteredAddrs := mixedAddrs[:1]

	v6v4Data := make([]*discoverd.Instance, 3)
	v6v4Addrs := make([]testAddr, 3)
	copy(v6v4Data, simpleData)
	copy(v6v4Addrs, simpleAddrs)
	v6v4Data[0], v6v4Addrs[0] = fakeStaticInstance("tcp", "fe80::bae8:56ff:fe46:243c", 22)

	longData := make([]*discoverd.Instance, 5)
	longAddrs := make([]testAddr, 5)
	copy(longData, simpleData)
	copy(longAddrs, simpleAddrs)
	longData[3], longAddrs[3] = fakeStaticInstance("tcp", "192.168.0.4", 83)
	longData[4], longAddrs[4] = fakeStaticInstance("tcp", "192.168.0.5", 84)

	dupeData := make([]*discoverd.Instance, 3)
	dupeAddrs := make([]testAddr, 3)
	copy(dupeData, simpleData)
	copy(dupeAddrs, simpleAddrs)
	dupeData[1], dupeAddrs[1] = fakeStaticInstance("tcp", "192.168.0.1", 85)

	emptyAll := map[uint16][]testAddr{
		dns.TypeA:    nil,
		dns.TypeAAAA: nil,
		dns.TypeANY:  nil,
		dns.TypeSRV:  nil,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	simpleQs := map[uint16][]testAddr{
		dns.TypeA:    simpleAddrs,
		dns.TypeAAAA: nil,
		dns.TypeANY:  simpleAddrs,
		dns.TypeSRV:  simpleAddrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	udpFiltered := map[uint16][]testAddr{
		dns.TypeA:    filteredAddrs,
		dns.TypeAAAA: nil,
		dns.TypeANY:  filteredAddrs,
		dns.TypeSRV:  filteredAddrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	mixedQs := map[uint16][]testAddr{
		dns.TypeA:    mixedAddrs,
		dns.TypeAAAA: nil,
		dns.TypeANY:  mixedAddrs,
		dns.TypeSRV:  mixedAddrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	v6v4Qs := map[uint16][]testAddr{
		dns.TypeA:    v6v4Addrs[1:],
		dns.TypeAAAA: v6v4Addrs[:1],
		dns.TypeANY:  v6v4Addrs,
		dns.TypeSRV:  v6v4Addrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	instanceQs := map[uint16][]testAddr{
		dns.TypeA:    simpleAddrs[:1],
		dns.TypeAAAA: nil,
		dns.TypeANY:  simpleAddrs[:1],
		dns.TypeSRV:  simpleAddrs[:1],
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	v6instanceQs := map[uint16][]testAddr{
		dns.TypeA:    nil,
		dns.TypeAAAA: v6v4Addrs[:1],
		dns.TypeANY:  v6v4Addrs[:1],
		dns.TypeSRV:  v6v4Addrs[:1],
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	longQs := map[uint16][]testAddr{
		dns.TypeA:    longAddrs,
		dns.TypeAAAA: nil,
		dns.TypeANY:  longAddrs,
		dns.TypeSRV:  longAddrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	dupeQs := map[uint16][]testAddr{
		dns.TypeA:    dupeAddrs[1:],
		dns.TypeAAAA: nil,
		dns.TypeANY:  dupeAddrs[1:],
		dns.TypeSRV:  dupeAddrs,
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	leaderQs := map[uint16][]testAddr{
		dns.TypeA:    simpleAddrs[:1],
		dns.TypeAAAA: nil,
		dns.TypeANY:  simpleAddrs[:1],
		dns.TypeSRV:  simpleAddrs[:1],
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}
	v6LeaderQs := map[uint16][]testAddr{
		dns.TypeA:    nil,
		dns.TypeAAAA: v6v4Addrs[:1],
		dns.TypeANY:  v6v4Addrs[:1],
		dns.TypeSRV:  v6v4Addrs[:1],
		dns.TypeSOA:  nil,
		dns.TypeTXT:  nil,
	}

	tests := []test{
		{
			name:   "non-existent service NXDOMAIN",
			domain: "b.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "non-existent service RFC2782 NXDOMAIN",
			domain: "_b._tcp.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "non-existent instance with existing service NXDOMAIN",
			domain: "foo.a._i.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "non-existent instance with non-existent service NXDOMAIN",
			domain: "foo.b._i.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "bad domain NXDOMAIN",
			domain: "asdf.b.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "leader NXDOMAIN",
			domain: "leader.b.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "leader with non-existent service NXDOMAIN",
			domain: "leader.b.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "empty service",
			domain: "a.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "empty 2782",
			domain: "_a._tcp.discoverd.",
			qs:     emptyAll,
		},
		{
			name:   "2782 no proto matches",
			domain: "_a._foo.discoverd.",
			data:   simpleData,
			qs:     emptyAll,
		},
		{
			name:   "2782 full proto matches",
			domain: "_a._tcp.discoverd.",
			data:   simpleData,
			qs:     simpleQs,
		},
		{
			name:   "2782 some proto matches",
			domain: "_a._udp.discoverd.",
			data:   mixedData,
			qs:     udpFiltered,
		},
		{
			name:   "service mixed protocols",
			domain: "a.discoverd.",
			data:   mixedData,
			qs:     mixedQs,
		},
		{
			name:   "service mixed v4/v6",
			domain: "a.discoverd.",
			data:   v6v4Data,
			qs:     v6v4Qs,
		},
		{
			name:   "2782 mixed v4/v6",
			domain: "_a._tcp.discoverd.",
			data:   v6v4Data,
			qs:     v6v4Qs,
		},
		{
			name:   "instance",
			domain: fmt.Sprintf("%s.a._i.discoverd.", simpleData[0].ID),
			data:   simpleData,
			qs:     instanceQs,
		},
		{
			name:   "v6 instance",
			domain: fmt.Sprintf("%s.a._i.discoverd.", v6v4Data[0].ID),
			data:   v6v4Data,
			qs:     v6instanceQs,
		},
		{
			name:   "service udp limit",
			domain: "a.discoverd.",
			data:   longData,
			qs:     longQs,
		},
		{
			name:   "2782 udp limit",
			domain: "_a._tcp.discoverd.",
			data:   longData,
			qs:     longQs,
		},
		{
			name:   "service duplicate IPs",
			domain: "a.discoverd.",
			data:   dupeData,
			qs:     dupeQs,
		},
		{
			name:   "2782 duplicate IPs",
			domain: "_a._tcp.discoverd.",
			data:   dupeData,
			qs:     dupeQs,
		},
		{
			name:   "leader",
			domain: "leader.a.discoverd.",
			data:   simpleData,
			qs:     leaderQs,
		},
		{
			name:   "v6 leader",
			domain: "leader.a.discoverd.",
			data:   v6v4Data[:1],
			qs:     v6LeaderQs,
		},
	}

	// Run all of the tests with TCP as well as UDP
	for i, t := range tests {
		tests[i].net = "udp"
		t.net = "tcp"
		tests = append(tests, t)
	}
	// Run all of the tests again to test case sensitivity
	for _, t := range tests {
		t.domain = strings.ToUpper(t.domain)
		tests = append(tests, t)
	}

	for _, t := range tests {
		func() {
			srv := s.newServer(c, []string{"8.8.8.8", "8.8.4.4"})
			defer srv.Close()
			srv.SetStore(&DNSServerStore{
				InstancesFn: func(service string) ([]*discoverd.Instance, error) {
					if service == "a" {
						if len(t.data) == 0 {
							return []*discoverd.Instance{}, nil
						} else {
							return t.data, nil
						}
					}
					return nil, nil
				},
				ServiceLeaderFn: func(service string) (*discoverd.Instance, error) {
					if service == "a" && len(t.data) > 0 {
						return t.data[0], nil
					}
					return nil, nil
				},
			})

			client := &dns.Client{Net: t.net}
			for q, addrs := range t.qs {
				c.Logf("+ %s: %s - %s - %s", t.domain, t.net, t.name, dns.TypeToString[q])

				// exchange the question
				req := &dns.Msg{}
				req.SetQuestion(t.domain, q)
				addr := srv.UDPAddr
				if t.net == "tcp" {
					addr = srv.TCPAddr
				}
				res, _, err := client.Exchange(req, addr)
				c.Assert(err, IsNil)

				if strings.Contains(t.name, "NXDOMAIN") {
					// if this is a nxdomain test we just need to ensure we got an nxdomain
					c.Assert(res.Rcode, Equals, dns.RcodeNameError)
					c.Assert(res.Extra, HasLen, 0)
					c.Assert(res.Answer, HasLen, 0)
					assertSOA(c, res.Ns)
					continue
				}

				// UDP responses only include up to three responses
				truncated := t.net == "udp" && len(addrs) > 3

				c.Assert(res.Rcode, Equals, dns.RcodeSuccess)
				switch {
				case q == dns.TypeANY:
					if truncated {
						// three SRV records plus three A/AAAA records
						c.Assert(res.Answer, HasLen, 6)
					} else {
						// SRV + A/AAAA records
						c.Assert(res.Answer, HasLen, len(addrs)*2)
					}
				case q == dns.TypeSOA:
					// the only response to a SOA question should be an SOA answer
					assertSOA(c, res.Answer)
					continue
				case len(addrs) == 0:
					// empty responses include an SOA answer in the authority section
					assertSOA(c, res.Ns)
					c.Assert(res.Answer, HasLen, 0)
				default:
					if truncated {
						c.Assert(res.Answer, HasLen, 3)
					} else {
						c.Assert(res.Answer, HasLen, len(addrs))
					}
				}

				// build a list of all A/AAAA and SRV records received
				ips := make(map[string]struct{}, len(addrs))
				srv := make(map[string]struct{}, len(addrs))
				for _, rr := range res.Answer {
					switch v := rr.(type) {
					case *dns.A:
						ips[v.A.String()] = struct{}{}
						c.Assert(v.A.To4(), NotNil)
						c.Assert(v.Hdr.Name, Equals, t.domain)
						c.Assert(v.Hdr.Rrtype, Equals, dns.TypeA)
					case *dns.AAAA:
						ips[v.AAAA.String()] = struct{}{}
						c.Assert(v.AAAA.To4(), IsNil)
						c.Assert(v.Hdr.Name, Equals, t.domain)
						c.Assert(v.Hdr.Rrtype, Equals, dns.TypeAAAA)
					case *dns.SRV:
						srv[fmt.Sprintf("%s:%d", v.Target, v.Port)] = struct{}{}
						c.Assert(v.Hdr.Name, Equals, t.domain)
						c.Assert(v.Hdr.Rrtype, Equals, dns.TypeSRV)
						c.Assert(v.Weight, Equals, uint16(1))
						c.Assert(v.Priority, Equals, uint16(1))
					default:
						c.Fatalf("unexpected record in answer %#v", v)
					}
				}

				// ensure that we got the expected A/AAAA records
				if q == dns.TypeANY || q == dns.TypeA || q == dns.TypeAAAA {
					if truncated {
						c.Assert(ips, HasLen, 3)
					} else {
						c.Assert(ips, HasLen, len(addrs))
					}

					var found int
					for _, addr := range addrs {
						if _, ok := ips[addr.IP.String()]; ok {
							found++
						}
					}

					if truncated {
						c.Assert(found, Equals, 3)
					} else {
						c.Assert(found, Equals, len(addrs))
					}
				} else {
					c.Assert(ips, HasLen, 0)
				}

				// ensure that we got the expected SRV records
				if q == dns.TypeANY || q == dns.TypeSRV {
					if truncated {
						c.Assert(srv, HasLen, 3)
					} else {
						c.Assert(srv, HasLen, len(addrs))
					}

					if !strings.Contains(t.name, "duplicate") {
						var found int
						for _, addr := range addrs {
							key := fmt.Sprintf("%s.a._i.discoverd.:%d", addr.ID, addr.Port)
							if strings.Contains(t.name, "instance") || strings.Contains(t.name, "leader") {
								key = fmt.Sprintf("%s:%d", t.domain, addr.Port)
							}
							if _, ok := srv[key]; ok {
								found++
							}
						}

						if truncated {
							c.Assert(found, Equals, 3)
						} else {
							c.Assert(found, Equals, len(addrs))
						}
					}
				} else {
					c.Assert(srv, HasLen, 0)
				}

				// responses to SRV questions over TCP get instance records in
				// the extra section so that another lookup is not needed to get
				// the IP addresses
				if t.net == "tcp" && q == dns.TypeSRV {
					c.Assert(res.Extra, HasLen, len(addrs))
					extraIPs := make(map[string]string, len(res.Extra))
					for _, rr := range res.Extra {
						switch v := rr.(type) {
						case *dns.A:
							c.Assert(v.Hdr.Rrtype, Equals, dns.TypeA)
							id := strings.TrimSuffix(strings.ToLower(v.Hdr.Name), ".a._i.discoverd.")
							extraIPs[id] = v.A.String()
						case *dns.AAAA:
							c.Assert(v.Hdr.Rrtype, Equals, dns.TypeAAAA)
							id := strings.TrimSuffix(strings.ToLower(v.Hdr.Name), ".a._i.discoverd.")
							extraIPs[id] = v.AAAA.String()
						default:
							c.Fatalf("unexpected record in extra %#v", v)
						}
					}
					for _, addr := range addrs {
						if strings.Contains(t.name, "leader") {
							c.Assert(extraIPs[strings.ToLower(t.domain)], Equals, addr.IP.String())
						} else {
							c.Assert(extraIPs[addr.ID], Equals, addr.IP.String())
						}
					}
				} else {
					c.Assert(res.Extra, HasLen, 0)
				}
			}
		}()
	}
}

func assertSOA(c *C, rrs []dns.RR) {
	c.Assert(rrs, HasLen, 1)
	c.Assert(rrs[0], FitsTypeOf, &dns.SOA{})
	soa := rrs[0].(*dns.SOA)
	c.Assert(soa.Hdr.Name, Equals, "discoverd.")
	c.Assert(soa.Hdr.Rrtype, Equals, dns.TypeSOA)
}

var dnsIndex uint64

func fakeStaticInstance(proto, ip string, port uint16) (*discoverd.Instance, testAddr) {
	inst := &discoverd.Instance{
		Proto: proto,
		Addr:  net.JoinHostPort(ip, strconv.Itoa(int(port))),
		Index: atomic.AddUint64(&dnsIndex, 1),
	}

	digest := md5.Sum([]byte(inst.Proto + "-" + inst.Addr))
	inst.ID = hex.EncodeToString(digest[:])
	netIP := net.ParseIP(ip)
	return inst, testAddr{netIP, port, inst.ID}
}

type testAddr struct {
	IP   net.IP
	Port uint16
	ID   string
}

// DNSServerStore represents a mock implementation of DNSServer.Store.
type DNSServerStore struct {
	InstancesFn     func(service string) ([]*discoverd.Instance, error)
	ServiceLeaderFn func(service string) (*discoverd.Instance, error)
}

func (s *DNSServerStore) Instances(service string) ([]*discoverd.Instance, error) {
	return s.InstancesFn(service)
}

func (s *DNSServerStore) ServiceLeader(service string) (*discoverd.Instance, error) {
	return s.ServiceLeaderFn(service)
}
