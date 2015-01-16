package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/miekg/dns"
	"github.com/flynn/flynn/discoverd2/client"
)

type DNSSuite struct {
	state   *State
	srv     *DNSServer
	cleanup []func()
	addr    string
}

var _ = Suite(&DNSSuite{})

// recurse with valid recursors
// recurse with invalid recursors
// recurse without any recursors

func (s *DNSSuite) SetUpTest(c *C) {
	s.cleanup = nil
	s.state = NewState()
	s.addr = "127.0.0.1:5553"

	s.srv = &DNSServer{
		UDPAddr:   s.addr,
		TCPAddr:   s.addr,
		Store:     s.state,
		Recursors: []string{"8.8.8.8", "8.8.4.4"},
	}
	c.Assert(s.srv.ListenAndServe(), IsNil)
	s.cleanup = append(s.cleanup, func() { s.srv.Close() })

	s.state.AddService("a")
}

func (s *DNSSuite) TearDownTest(c *C) {
	for i := len(s.cleanup); i != 0; i-- {
		s.cleanup[i-1]()
	}
}

func (s *DNSSuite) TestRecursor(c *C) {
	for _, net := range []string{"tcp", "udp"} {
		c.Log(net)
		client := &dns.Client{
			ReadTimeout: 10 * time.Second,
			Net:         net,
		}

		// Valid request
		s.srv.Recursors = []string{"8.8.8.8:53"}
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)
		res, _, err := client.Exchange(msg, s.addr)
		c.Assert(err, IsNil)
		c.Assert(res.Rcode, Equals, dns.RcodeSuccess)
		c.Assert(len(res.Answer) > 0, Equals, true)

		// Failing recursor fallback
		s.srv.Recursors = []string{"127.1.1.1:55", "8.8.8.8:53"}
		res, _, err = client.Exchange(msg, s.addr)
		c.Assert(err, IsNil)
		c.Assert(res.Rcode, Equals, dns.RcodeSuccess)
		c.Assert(len(res.Answer) > 0, Equals, true)

		// All failing
		s.srv.Recursors = []string{"127.1.1.1:55"}
		res, _, err = client.Exchange(msg, s.addr)
		c.Assert(err, IsNil)
		c.Assert(res.Rcode, Equals, dns.RcodeServerFailure)
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
		if len(t.data) == 0 {
			// nil deletes the service, so use an empty slice
			s.state.SetService("a", []*discoverd.Instance{})
		} else {
			s.state.SetService("a", t.data)
		}
		client := &dns.Client{Net: t.net}
		for q, addrs := range t.qs {
			c.Logf("+ %s: %s - %s - %s", t.domain, t.net, t.name, dns.TypeToString[q])

			// exchange the question
			req := &dns.Msg{}
			req.SetQuestion(t.domain, q)
			res, _, err := client.Exchange(req, s.addr)
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
	inst.ID = md5sum(inst.Proto + "-" + inst.Addr)
	netIP := net.ParseIP(ip)
	return inst, testAddr{netIP, port, inst.ID}
}

type testAddr struct {
	IP   net.IP
	Port uint16
	ID   string
}
