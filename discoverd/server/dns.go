package server

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/miekg/dns"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/vanillahsu/go_reuseport"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
)

type DNSServer struct {
	UDPAddr   string
	TCPAddr   string
	Domain    string
	Recursors []string

	Store interface {
		Instances(service string) ([]*discoverd.Instance, error)
		ServiceLeader(service string) (*discoverd.Instance, error)
	}

	servers []*dns.Server
}

const maxUDPRecords = 3
const dnsDomain = "discoverd."

func (srv *DNSServer) ListenAndServe() error {
	if srv.Store == nil {
		panic("missing Store")
	}
	if srv.Domain == "" {
		srv.Domain = dnsDomain
	}
	if err := srv.validateRecursors(); err != nil {
		return err
	}

	api := dnsAPI{srv}
	mux := dns.NewServeMux()
	mux.HandleFunc(srv.Domain, api.ServiceLookup)
	if len(srv.Recursors) > 0 {
		mux.HandleFunc(".", api.Recurse)
	}

	errors := make(chan error, 4)
	done := func() { errors <- nil }

	if srv.UDPAddr != "" {
		l, err := reuseport.NewReusablePortPacketConn("udp4", srv.UDPAddr)
		if err != nil {
			return err
		}
		srv.UDPAddr = l.(*net.UDPConn).LocalAddr().String()
		server := &dns.Server{
			Net:               "udp",
			PacketConn:        l,
			Handler:           mux,
			NotifyStartedFunc: done,
		}
		go func() { errors <- server.ActivateAndServe() }()
		srv.servers = append(srv.servers, server)
	}

	if srv.TCPAddr != "" {
		l, err := net.Listen("tcp", srv.TCPAddr)
		if err != nil {
			return err
		}
		srv.TCPAddr = l.Addr().String()
		server := &dns.Server{
			Net:               "tcp",
			Listener:          l,
			Handler:           mux,
			NotifyStartedFunc: done,
		}
		go func() { errors <- server.ActivateAndServe() }()
		srv.servers = append(srv.servers, server)
	}

	for range srv.servers {
		if err := <-errors; err != nil {
			return err
		}
	}
	return nil
}

func (srv *DNSServer) validateRecursors() error {
	for i, r := range srv.Recursors {
		_, _, err := net.SplitHostPort(r)
		if e, ok := err.(*net.AddrError); ok {
			switch e.Err {
			case "missing port in address":
				r = r + ":53"
			case "too many colons in address":
				// Assume a bare IPv6 address
				r = fmt.Sprintf("[%s]:53", r)
			}
		} else if err != nil {
			return fmt.Errorf("discoverd: invalid recursor address %s: %s", r, err)
		}
		addr, err := net.ResolveTCPAddr("tcp", r)
		if err != nil {
			return fmt.Errorf("discoverd: unable to resolve recursor address %s: %s", r, err)
		}
		srv.Recursors[i] = addr.String()
	}
	return nil
}

func (srv *DNSServer) Close() error {
	var err error
	for _, s := range srv.servers {
		e := s.Shutdown()
		if err == nil {
			err = e
		}
	}
	return err
}

type dnsAPI struct {
	*DNSServer
}

func (d dnsAPI) Recurse(w dns.ResponseWriter, req *dns.Msg) {
	var client dns.Client

	if isTCP(w.RemoteAddr()) {
		client.Net = "tcp"
	}

	for _, recursor := range d.Recursors {
		res, _, err := client.Exchange(req, recursor)
		if err != nil {
			continue
		}
		w.WriteMsg(res)
		return
	}

	// Return SERVFAIL
	res := &dns.Msg{}
	res.RecursionAvailable = true
	res.SetRcode(req, dns.RcodeServerFailure)
	w.WriteMsg(res)
}

func (d dnsAPI) ServiceLookup(w dns.ResponseWriter, req *dns.Msg) {
	qName := req.Question[0].Name
	qType := req.Question[0].Qtype
	name := strings.TrimSuffix(strings.ToLower(dns.Fqdn(qName)), d.Domain)
	labels := dns.SplitDomainName(name)
	tcp := isTCP(w.RemoteAddr())

	res := &dns.Msg{}
	res.Authoritative = true
	res.RecursionAvailable = len(d.Recursors) > 0
	res.SetReply(req)
	defer func() {
		if res.Rcode == dns.RcodeSuccess && qType == dns.TypeSOA {
			// SOA answer if requested. at the end of the request to ensure we didn't hit NXDOMAIN
			res.Answer = []dns.RR{d.soaRecord()}
		}
		if len(res.Answer) == 0 {
			// Add authority section with SOA if the answer has no items
			res.Ns = []dns.RR{d.soaRecord()}
		}
		w.WriteMsg(res)
	}()

	nxdomain := func() { res.SetRcode(req, dns.RcodeNameError) }

	var service string
	var proto string
	var instanceID string
	var leader bool
	switch {
	case len(labels) == 1:
		// normal lookup
		service = labels[0]
	case len(labels) == 2 && strings.HasPrefix(labels[0], "_") && strings.HasPrefix(labels[1], "_"):
		// RFC 2782 request looks like _postgres._tcp
		service = labels[0][1:]
		proto = labels[1][1:]
	case len(labels) == 3 && labels[2] == "_i":
		// address lookup for instance in RFC 2782 SRV record
		service = labels[1]
		instanceID = labels[0]
	case len(labels) == 2 && labels[0] == "leader":
		// leader lookup
		leader = true
		service = labels[1]
	default:
		nxdomain()
		return
	}

	var instances []*discoverd.Instance
	if !leader {
		a, err := d.Store.Instances(service)
		if err != nil {
			log.Println("discoverd: dns: cannot retrieve instances: %s", err)
			nxdomain()
			return
		} else if a == nil {
			nxdomain()
			return
		}
		instances = a
	}

	if leader || instanceID != "" {
		// we're doing a lookup for a single instance
		var resInst *discoverd.Instance
		if leader {
			sl, err := d.Store.ServiceLeader(service)
			if err != nil {
				log.Println("discoverd: dns: cannot retrieve service leader: %s", err)
				nxdomain()
				return
			}
			resInst = sl
		} else {
			for _, inst := range instances {
				if inst.ID == instanceID {
					resInst = inst
					break
				}
			}
		}
		if resInst == nil {
			nxdomain()
			return
		}

		addr := parseAddr(resInst)
		if qType != dns.TypeA && qType != dns.TypeAAAA && qType != dns.TypeANY && qType != dns.TypeSRV ||
			addr.IPv4 == nil && qType == dns.TypeA ||
			addr.IPv6 == nil && qType == dns.TypeAAAA {
			// no results if we're looking up an record that doesn't match the
			// request type or the type is incorrect
			return
		}
		res.Answer = make([]dns.RR, 0, 2)
		if qType != dns.TypeSRV {
			res.Answer = append(res.Answer, addrRecord(qName, addr))
		}
		if qType == dns.TypeSRV || qType == dns.TypeANY {
			res.Answer = append(res.Answer, d.srvRecord(qName, service, addr, false))
		}
		if tcp && qType == dns.TypeSRV {
			res.Extra = []dns.RR{addrRecord(qName, addr)}
		}
		return
	}

	if qType == dns.TypeSOA {
		// We don't need to do any more processing, as NXDOMAIN can't be reached
		// beyond this point, the SOA answer is added in the deferred function
		// above
		return
	}

	addrs := make([]*addrData, 0, len(instances))
	added := make(map[string]struct{}, len(instances))
	for _, inst := range instances {
		if proto != "" && inst.Proto != proto {
			continue
		}
		addr := parseAddr(inst)
		if _, ok := added[addr.String]; ok {
			continue
		}
		if addr.IPv4 == nil && qType == dns.TypeA || addr.IPv6 == nil && qType == dns.TypeAAAA {
			// Skip instance if we have an IPv6 address but want IPv4 or vice versa
			continue
		}
		if qType != dns.TypeSRV {
			// skip duplicate IPs if we're not doing an SRV lookup
			added[addr.String] = struct{}{}
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		// return empty response
		return
	}
	shuffle(addrs)

	// Truncate the response if we're using UDP
	if !tcp && len(addrs) > maxUDPRecords {
		addrs = addrs[:maxUDPRecords]
	}

	res.Answer = make([]dns.RR, 0, len(addrs)*2)
	for _, addr := range addrs {
		if qType == dns.TypeANY || qType == dns.TypeA || qType == dns.TypeAAAA {
			res.Answer = append(res.Answer, addrRecord(qName, addr))
		}
	}
	for _, addr := range addrs {
		if qType == dns.TypeANY || qType == dns.TypeSRV {
			res.Answer = append(res.Answer, d.srvRecord(qName, service, addr, true))
		}
	}

	if qType == dns.TypeSRV && tcp {
		// Add extra records mapping instance IDs to addresses
		res.Extra = make([]dns.RR, len(addrs))
		for i, addr := range addrs {
			res.Extra[i] = addrRecord(d.instanceDomain(service, addr.ID), addr)
		}
	}
}

func (d dnsAPI) soaRecord() dns.RR {
	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   d.Domain,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
		},
		Ns:      "ns." + d.Domain,
		Mbox:    "postmaster." + d.Domain,
		Serial:  uint32(time.Now().Unix()),
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
	}
}

func (d dnsAPI) srvRecord(name, service string, addr *addrData, instTarget bool) dns.RR {
	r := &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
		},
		Priority: 1,
		Weight:   1,
		Port:     addr.Port,
		Target:   name,
	}
	if instTarget {
		r.Target = d.instanceDomain(service, addr.ID)
	}
	return r
}

func (d dnsAPI) instanceDomain(service, id string) string {
	return fmt.Sprintf("%s.%s._i.%s", id, service, d.Domain)
}

func addrRecord(name string, addr *addrData) dns.RR {
	if addr.IPv6 != nil {
		return &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
			},
			AAAA: addr.IPv6,
		}
	}
	return &dns.A{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
		},
		A: addr.IPv4,
	}
}

type addrData struct {
	IPv6   net.IP
	IPv4   net.IP
	String string
	Port   uint16
	ID     string
}

func parseAddr(inst *discoverd.Instance) *addrData {
	res := &addrData{ID: inst.ID}
	ip, port, _ := net.SplitHostPort(inst.Addr)
	res.String = ip
	portInt, _ := strconv.Atoi(port)
	res.Port = uint16(portInt)
	ipBytes := net.ParseIP(ip)
	res.IPv4 = ipBytes.To4()
	if res.IPv4 == nil {
		res.IPv6 = ipBytes
	}
	return res
}

func shuffle(s []*addrData) []*addrData {
	for i := len(s) - 1; i > 0; i-- {
		j := random.Math.Intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
	return s
}

func isTCP(addr net.Addr) bool {
	_, ok := addr.(*net.TCPAddr)
	return ok
}
