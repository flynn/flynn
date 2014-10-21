// The ec2test package implements a fake EC2 provider with
// the capability of inducing errors on any given operation,
// and retrospectively determining what operations have been
// carried out.
package ec2test

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/ec2"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var b64 = base64.StdEncoding

// Action represents a request that changes the ec2 state.
type Action struct {
	RequestId string

	// Request holds the requested action as a url.Values instance
	Request url.Values

	// If the action succeeded, Response holds the value that
	// was marshalled to build the XML response for the request.
	Response interface{}

	// If the action failed, Err holds an error giving details of the failure.
	Err *ec2.Error
}

// TODO possible other things:
// - some virtual time stamp interface, so a client
// can ask for all actions after a certain virtual time.

// Server implements an EC2 simulator for use in testing.
type Server struct {
	url      string
	listener net.Listener
	mu       sync.Mutex
	reqs     []*Action

	instances            map[string]*Instance      // id -> instance
	reservations         map[string]*reservation   // id -> reservation
	groups               map[string]*securityGroup // id -> group
	vpcs                 map[string]*vpc           // id -> vpc
	subnets              map[string]*subnet        // id -> subnet
	ifaces               map[string]*iface         // id -> iface
	attachments          map[string]*attachment    // id -> attachment
	maxId                counter
	reqId                counter
	reservationId        counter
	groupId              counter
	vpcId                counter
	dhcpOptsId           counter
	subnetId             counter
	ifaceId              counter
	attachId             counter
	initialInstanceState ec2.InstanceState
}

// reservation holds a simulated ec2 reservation.
type reservation struct {
	id        string
	instances map[string]*Instance
	groups    []*securityGroup
}

// instance holds a simulated ec2 instance
type Instance struct {
	seq        int
	dnsNameSet bool
	// UserData holds the data that was passed to the RunInstances request
	// when the instance was started.
	UserData    []byte
	imageId     string
	reservation *reservation
	instType    string
	state       ec2.InstanceState
	subnetId    string
	vpcId       string
}

// permKey represents permission for a given security
// group or IP address (but not both) to access a given range of
// ports. Equality of permKeys is used in the implementation of
// permission sets, relying on the uniqueness of securityGroup
// instances.
type permKey struct {
	protocol string
	fromPort int
	toPort   int
	group    *securityGroup
	ipAddr   string
}

// securityGroup holds a simulated ec2 security group.
// Instances of securityGroup should only be created through
// Server.createSecurityGroup to ensure that groups can be
// compared by pointer value.
type securityGroup struct {
	id          string
	name        string
	description string
	vpcId       string

	perms map[permKey]bool
}

func (g *securityGroup) ec2SecurityGroup() ec2.SecurityGroup {
	return ec2.SecurityGroup{
		Name: g.name,
		Id:   g.id,
	}
}

func (g *securityGroup) matchAttr(attr, value string) (ok bool, err error) {
	switch attr {
	case "description":
		return g.description == value, nil
	case "group-id":
		return g.id == value, nil
	case "group-name":
		return g.name == value, nil
	case "ip-permission.cidr":
		return g.hasPerm(func(k permKey) bool { return k.ipAddr == value }), nil
	case "ip-permission.group-name":
		return g.hasPerm(func(k permKey) bool {
			return k.group != nil && k.group.name == value
		}), nil
	case "ip-permission.from-port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return false, err
		}
		return g.hasPerm(func(k permKey) bool { return k.fromPort == port }), nil
	case "ip-permission.to-port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return false, err
		}
		return g.hasPerm(func(k permKey) bool { return k.toPort == port }), nil
	case "ip-permission.protocol":
		return g.hasPerm(func(k permKey) bool { return k.protocol == value }), nil
	case "owner-id":
		return value == ownerId, nil
	case "vpc-id":
		return g.vpcId == value, nil
	}
	return false, fmt.Errorf("unknown attribute %q", attr)
}

func (g *securityGroup) hasPerm(test func(k permKey) bool) bool {
	for k := range g.perms {
		if test(k) {
			return true
		}
	}
	return false
}

// ec2Perms returns the list of EC2 permissions granted
// to g. It groups permissions by port range and protocol.
func (g *securityGroup) ec2Perms() (perms []ec2.IPPerm) {
	// The grouping is held in result. We use permKey for convenience,
	// (ensuring that the group and ipAddr of each key is zero). For
	// each protocol/port range combination, we build up the permission
	// set in the associated value.
	result := make(map[permKey]*ec2.IPPerm)
	for k := range g.perms {
		groupKey := k
		groupKey.group = nil
		groupKey.ipAddr = ""

		ec2p := result[groupKey]
		if ec2p == nil {
			ec2p = &ec2.IPPerm{
				Protocol: k.protocol,
				FromPort: k.fromPort,
				ToPort:   k.toPort,
			}
			result[groupKey] = ec2p
		}
		if k.group != nil {
			ec2p.SourceGroups = append(ec2p.SourceGroups,
				ec2.UserSecurityGroup{
					Id:      k.group.id,
					Name:    k.group.name,
					OwnerId: ownerId,
				})
		} else {
			ec2p.SourceIPs = append(ec2p.SourceIPs, k.ipAddr)
		}
	}
	for _, ec2p := range result {
		perms = append(perms, *ec2p)
	}
	return
}

type vpc struct {
	ec2.VPC
}

func (v *vpc) matchAttr(attr, value string) (ok bool, err error) {
	switch attr {
	case "cidr":
		return v.CIDRBlock == value, nil
	case "state":
		return v.State == value, nil
	case "vpc-id":
		return v.Id == value, nil
	case "tag", "tag-key", "tag-value", "dhcp-options-id", "isDefault":
		return false, fmt.Errorf("%q filter is not implemented", attr)
	}
	return false, fmt.Errorf("unknown attribute %q", attr)
}

type subnet struct {
	ec2.Subnet
}

func (s *subnet) matchAttr(attr, value string) (ok bool, err error) {
	switch attr {
	case "cidr":
		return s.CIDRBlock == value, nil
	case "availability-zone":
		return s.AvailZone == value, nil
	case "state":
		return s.State == value, nil
	case "subnet-id":
		return s.Id == value, nil
	case "vpc-id":
		return s.VPCId == value, nil
	case "tag", "tag-key", "tag-value", "available-ip-address-count", "defaultForAz":
		return false, fmt.Errorf("%q filter not implemented", attr)
	}
	return false, fmt.Errorf("unknown attribute %q", attr)
}

type iface struct {
	ec2.NetworkInterface
}

func (i *iface) matchAttr(attr, value string) (ok bool, err error) {
	notImplemented := []string{
		"addresses.", "association.", "tag", "requester-",
		"attachment.", "source-dest-check", "mac-address",
		"group-", "description", "private-", "owner-id",
	}
	switch attr {
	case "availability-zone":
		return i.AvailZone == value, nil
	case "network-interface-id":
		return i.Id == value, nil
	case "status":
		return i.Status == value, nil
	case "subnet-id":
		return i.SubnetId == value, nil
	case "vpc-id":
		return i.VPCId == value, nil
	default:
		for _, item := range notImplemented {
			if strings.HasPrefix(attr, item) {
				return false, fmt.Errorf("%q filter not implemented", attr)
			}
		}
	}
	return false, fmt.Errorf("unknown attribute %q", attr)
}

type attachment struct {
	ec2.NetworkInterfaceAttachment
}

var actions = map[string]func(*Server, http.ResponseWriter, *http.Request, string) interface{}{
	"RunInstances":                  (*Server).runInstances,
	"TerminateInstances":            (*Server).terminateInstances,
	"DescribeInstances":             (*Server).describeInstances,
	"CreateSecurityGroup":           (*Server).createSecurityGroup,
	"DescribeSecurityGroups":        (*Server).describeSecurityGroups,
	"DeleteSecurityGroup":           (*Server).deleteSecurityGroup,
	"AuthorizeSecurityGroupIngress": (*Server).authorizeSecurityGroupIngress,
	"RevokeSecurityGroupIngress":    (*Server).revokeSecurityGroupIngress,
	"CreateVpc":                     (*Server).createVpc,
	"DeleteVpc":                     (*Server).deleteVpc,
	"DescribeVpcs":                  (*Server).describeVpcs,
	"CreateSubnet":                  (*Server).createSubnet,
	"DeleteSubnet":                  (*Server).deleteSubnet,
	"DescribeSubnets":               (*Server).describeSubnets,
	"CreateNetworkInterface":        (*Server).createIFace,
	"DeleteNetworkInterface":        (*Server).deleteIFace,
	"DescribeNetworkInterfaces":     (*Server).describeIFaces,
	"AttachNetworkInterface":        (*Server).attachIFace,
	"DetachNetworkInterface":        (*Server).detachIFace,
}

const ownerId = "9876"

// newAction allocates a new action and adds it to the
// recorded list of server actions.
func (srv *Server) newAction() *Action {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	a := new(Action)
	srv.reqs = append(srv.reqs, a)
	return a
}

// NewServer returns a new server.
func NewServer() (*Server, error) {
	srv := &Server{
		instances:            make(map[string]*Instance),
		groups:               make(map[string]*securityGroup),
		vpcs:                 make(map[string]*vpc),
		subnets:              make(map[string]*subnet),
		ifaces:               make(map[string]*iface),
		attachments:          make(map[string]*attachment),
		reservations:         make(map[string]*reservation),
		initialInstanceState: Pending,
	}

	// Add default security group.
	g := &securityGroup{
		name:        "default",
		description: "default group",
		id:          fmt.Sprintf("sg-%d", srv.groupId.next()),
	}
	g.perms = map[permKey]bool{
		permKey{
			protocol: "icmp",
			fromPort: -1,
			toPort:   -1,
			group:    g,
		}: true,
		permKey{
			protocol: "tcp",
			fromPort: 0,
			toPort:   65535,
			group:    g,
		}: true,
		permKey{
			protocol: "udp",
			fromPort: 0,
			toPort:   65535,
			group:    g,
		}: true,
	}
	srv.groups[g.id] = g

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("cannot listen on localhost: %v", err)
	}
	srv.listener = l

	srv.url = "http://" + l.Addr().String()

	// we use HandlerFunc rather than *Server directly so that we
	// can avoid exporting HandlerFunc from *Server.
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		srv.serveHTTP(w, req)
	}))
	return srv, nil
}

// Quit closes down the server.
func (srv *Server) Quit() {
	srv.listener.Close()
}

// SetInitialInstanceState sets the state that any new instances will be started in.
func (srv *Server) SetInitialInstanceState(state ec2.InstanceState) {
	srv.mu.Lock()
	srv.initialInstanceState = state
	srv.mu.Unlock()
}

// URL returns the URL of the server.
func (srv *Server) URL() string {
	return srv.url
}

// serveHTTP serves the EC2 protocol.
func (srv *Server) serveHTTP(w http.ResponseWriter, req *http.Request) {
	req.ParseForm()

	a := srv.newAction()
	a.RequestId = fmt.Sprintf("req%d", srv.reqId.next())
	a.Request = req.Form

	// Methods on Server that deal with parsing user data
	// may fail. To save on error handling code, we allow these
	// methods to call fatalf, which will panic with an *ec2.Error
	// which will be caught here and returned
	// to the client as a properly formed EC2 error.
	defer func() {
		switch err := recover().(type) {
		case *ec2.Error:
			a.Err = err
			err.RequestId = a.RequestId
			writeError(w, err)
		case nil:
		default:
			panic(err)
		}
	}()

	f := actions[req.Form.Get("Action")]
	if f == nil {
		fatalf(400, "InvalidParameterValue", "Unrecognized Action")
	}

	response := f(srv, w, req, a.RequestId)
	a.Response = response

	w.Header().Set("Content-Type", `xml version="1.0" encoding="UTF-8"`)
	xmlMarshal(w, response)
}

// Instance returns the instance for the given instance id.
// It returns nil if there is no such instance.
func (srv *Server) Instance(id string) *Instance {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.instances[id]
}

// writeError writes an appropriate error response.
// TODO how should we deal with errors when the
// error itself is potentially generated by backend-agnostic
// code?
func writeError(w http.ResponseWriter, err *ec2.Error) {
	// Error encapsulates an error returned by EC2.
	// TODO merge with ec2.Error when xml supports ignoring a field.
	type ec2error struct {
		Code      string // EC2 error code ("UnsupportedOperation", ...)
		Message   string // The human-oriented error message
		RequestId string
	}

	type Response struct {
		RequestId string
		Errors    []ec2error `xml:"Errors>Error"`
	}

	w.Header().Set("Content-Type", `xml version="1.0" encoding="UTF-8"`)
	w.WriteHeader(err.StatusCode)
	xmlMarshal(w, Response{
		RequestId: err.RequestId,
		Errors: []ec2error{{
			Code:    err.Code,
			Message: err.Message,
		}},
	})
}

// xmlMarshal is the same as xml.Marshal except that
// it panics on error. The marshalling should not fail,
// but we want to know if it does.
func xmlMarshal(w io.Writer, x interface{}) {
	if err := xml.NewEncoder(w).Encode(x); err != nil {
		panic(fmt.Errorf("error marshalling %#v: %v", x, err))
	}
}

// formToGroups parses a set of SecurityGroup form values
// as found in a RunInstances request, and returns the resulting
// slice of security groups.
// It calls fatalf if a group is not found.
func (srv *Server) formToGroups(form url.Values) []*securityGroup {
	var groups []*securityGroup
	for name, values := range form {
		switch {
		case strings.HasPrefix(name, "SecurityGroupId."):
			if g := srv.groups[values[0]]; g != nil {
				groups = append(groups, g)
			} else {
				fatalf(400, "InvalidGroup.NotFound", "unknown group id %q", values[0])
			}
		case strings.HasPrefix(name, "SecurityGroup."):
			var found *securityGroup
			for _, g := range srv.groups {
				if g.name == values[0] {
					found = g
				}
			}
			if found == nil {
				fatalf(400, "InvalidGroup.NotFound", "unknown group name %q", values[0])
			}
			groups = append(groups, found)
		}
	}
	return groups
}

// runInstances implements the EC2 RunInstances entry point.
func (srv *Server) runInstances(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	min := atoi(req.Form.Get("MinCount"))
	max := atoi(req.Form.Get("MaxCount"))
	if min < 0 || max < 1 {
		fatalf(400, "InvalidParameterValue", "bad values for MinCount or MaxCount")
	}
	if min > max {
		fatalf(400, "InvalidParameterCombination", "MinCount is greater than MaxCount")
	}
	var userData []byte
	if data := req.Form.Get("UserData"); data != "" {
		var err error
		userData, err = b64.DecodeString(data)
		if err != nil {
			fatalf(400, "InvalidParameterValue", "bad UserData value: %v", err)
		}
	}

	// TODO attributes still to consider:
	//    ImageId:                  accept anything, we can verify later
	//    KeyName                   ?
	//    InstanceType              ?
	//    KernelId                  ?
	//    RamdiskId                 ?
	//    AvailZone                 ?
	//    GroupName                 tag
	//    Monitoring                ignore?
	//    DisableAPITermination     bool
	//    ShutdownBehavior          string
	//    PrivateIPAddress          string

	srv.mu.Lock()
	defer srv.mu.Unlock()

	var vpcId string
	subnetId := req.Form.Get("SubnetId")
	if subnetId != "" {
		sub, found := srv.subnets[subnetId]
		if !found {
			fatalf(400, "InvalidSubnetID.NotFound", "subnet %s not found", subnetId)
		}
		vpcId = sub.VPCId
	}

	// make sure that form fields are correct before creating the reservation.
	instType := req.Form.Get("InstanceType")
	imageId := req.Form.Get("ImageId")

	r := srv.newReservation(srv.formToGroups(req.Form))

	var resp ec2.RunInstancesResp
	resp.RequestId = reqId
	resp.ReservationId = r.id
	resp.OwnerId = ownerId

	for i := 0; i < max; i++ {
		inst := srv.newInstance(r, instType, imageId, srv.initialInstanceState, subnetId, vpcId)
		inst.UserData = userData
		resp.Instances = append(resp.Instances, inst.ec2instance())
	}
	return &resp
}

func (srv *Server) group(group ec2.SecurityGroup) *securityGroup {
	if group.Id != "" {
		return srv.groups[group.Id]
	}
	for _, g := range srv.groups {
		if g.name == group.Name {
			return g
		}
	}
	return nil
}

// NewInstancesVPC creates n new VPC instances in srv with the given
// instance type, image ID, initial state, and security groups,
// belonging to the given vpcId and subnetId. If any group does not
// already exist, it will be created. NewInstancesVPC returns the ids
// of the new instances.
//
// If vpcId and subnetId are both empty, this call is equivalent to
// calling NewInstances.
func (srv *Server) NewInstancesVPC(vpcId, subnetId string, n int, instType string, imageId string, state ec2.InstanceState, groups []ec2.SecurityGroup) []string {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	rgroups := make([]*securityGroup, len(groups))
	for i, group := range groups {
		g := srv.group(group)
		if g == nil {
			fatalf(400, "InvalidGroup.NotFound", "no such group %v", g)
		}
		rgroups[i] = g
	}
	r := srv.newReservation(rgroups)

	ids := make([]string, n)
	for i := 0; i < n; i++ {
		inst := srv.newInstance(r, instType, imageId, state, subnetId, vpcId)
		ids[i] = inst.id()
	}
	return ids
}

// NewInstances creates n new instances in srv with the given instance
// type, image ID, initial state, and security groups. If any group
// does not already exist, it will be created. NewInstances returns
// the ids of the new instances.
func (srv *Server) NewInstances(n int, instType string, imageId string, state ec2.InstanceState, groups []ec2.SecurityGroup) []string {
	return srv.NewInstancesVPC("", "", n, instType, imageId, state, groups)
}

func (srv *Server) newInstance(r *reservation, instType string, imageId string, state ec2.InstanceState, subnetId, vpcId string) *Instance {
	inst := &Instance{
		seq:         srv.maxId.next(),
		instType:    instType,
		imageId:     imageId,
		state:       state,
		reservation: r,
	}
	if vpcId != "" && subnetId != "" {
		inst.vpcId = vpcId
		inst.subnetId = subnetId
	}
	id := inst.id()
	srv.instances[id] = inst
	r.instances[id] = inst
	return inst
}

func (srv *Server) newReservation(groups []*securityGroup) *reservation {
	r := &reservation{
		id:        fmt.Sprintf("r-%d", srv.reservationId.next()),
		instances: make(map[string]*Instance),
		groups:    groups,
	}

	srv.reservations[r.id] = r
	return r
}

func (srv *Server) terminateInstances(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	var resp ec2.TerminateInstancesResp
	resp.RequestId = reqId
	var insts []*Instance
	for attr, vals := range req.Form {
		if strings.HasPrefix(attr, "InstanceId.") {
			id := vals[0]
			inst := srv.instances[id]
			if inst == nil {
				fatalf(400, "InvalidInstanceID.NotFound", "no such instance id %q", id)
			}
			insts = append(insts, inst)
		}
	}
	for _, inst := range insts {
		resp.StateChanges = append(resp.StateChanges, inst.terminate())
	}
	return &resp
}

func (inst *Instance) id() string {
	return fmt.Sprintf("i-%d", inst.seq)
}

func (inst *Instance) terminate() (d ec2.InstanceStateChange) {
	d.PreviousState = inst.state
	inst.state = ShuttingDown
	d.CurrentState = inst.state
	d.InstanceId = inst.id()
	return d
}

func (inst *Instance) ec2instance() ec2.Instance {
	id := inst.id()
	// The first time the instance is returned, its DNSName
	// will be empty. The client should then refresh the instance.
	var dnsName string
	if inst.dnsNameSet {
		dnsName = fmt.Sprintf("%s.testing.invalid", id)
	} else {
		inst.dnsNameSet = true
	}
	return ec2.Instance{
		InstanceId:       id,
		InstanceType:     inst.instType,
		ImageId:          inst.imageId,
		DNSName:          dnsName,
		PrivateDNSName:   fmt.Sprintf("%s.internal.invalid", id),
		IPAddress:        fmt.Sprintf("8.0.0.%d", inst.seq%256),
		PrivateIPAddress: fmt.Sprintf("127.0.0.%d", inst.seq%256),
		State:            inst.state,
		VPCId:            inst.vpcId,
		SubnetId:         inst.subnetId,
		// TODO the rest
	}
}

func (inst *Instance) matchAttr(attr, value string) (ok bool, err error) {
	switch attr {
	case "architecture":
		return value == "i386", nil
	case "instance-id":
		return inst.id() == value, nil
	case "subnet-id":
		return inst.subnetId == value, nil
	case "vpc-id":
		return inst.vpcId == value, nil
	case "instance.group-id", "group-id":
		for _, g := range inst.reservation.groups {
			if g.id == value {
				return true, nil
			}
		}
		return false, nil
	case "instance.group-name", "group-name":
		for _, g := range inst.reservation.groups {
			if g.name == value {
				return true, nil
			}
		}
		return false, nil
	case "image-id":
		return value == inst.imageId, nil
	case "instance-state-code":
		code, err := strconv.Atoi(value)
		if err != nil {
			return false, err
		}
		return code&0xff == inst.state.Code, nil
	case "instance-state-name":
		return value == inst.state.Name, nil
	}
	return false, fmt.Errorf("unknown attribute %q", attr)
}

var (
	Pending      = ec2.InstanceState{0, "pending"}
	Running      = ec2.InstanceState{16, "running"}
	ShuttingDown = ec2.InstanceState{32, "shutting-down"}
	Terminated   = ec2.InstanceState{16, "terminated"}
	Stopped      = ec2.InstanceState{16, "stopped"}
)

func (srv *Server) createSecurityGroup(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	name := req.Form.Get("GroupName")
	if name == "" {
		fatalf(400, "InvalidParameterValue", "empty security group name")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.group(ec2.SecurityGroup{Name: name}) != nil {
		fatalf(400, "InvalidGroup.Duplicate", "group %q already exists", name)
	}
	g := &securityGroup{
		name:        name,
		description: req.Form.Get("GroupDescription"),
		id:          fmt.Sprintf("sg-%d", srv.groupId.next()),
		perms:       make(map[permKey]bool),
	}
	vpcId := req.Form.Get("VpcId")
	if vpcId != "" {
		g.vpcId = vpcId
	}
	srv.groups[g.id] = g
	// we define a local type for this because ec2.CreateSecurityGroupResp
	// contains SecurityGroup, but the response to this request
	// should not contain the security group name.
	type CreateSecurityGroupResponse struct {
		RequestId string `xml:"requestId"`
		Return    bool   `xml:"return"`
		GroupId   string `xml:"groupId"`
	}
	r := &CreateSecurityGroupResponse{
		RequestId: reqId,
		Return:    true,
		GroupId:   g.id,
	}
	return r
}

func (srv *Server) notImplemented(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	fatalf(500, "InternalError", "not implemented")
	panic("not reached")
}

func (srv *Server) describeInstances(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	insts := make(map[*Instance]bool)
	for name, vals := range req.Form {
		if !strings.HasPrefix(name, "InstanceId.") {
			continue
		}
		inst := srv.instances[vals[0]]
		if inst == nil {
			fatalf(400, "InvalidInstanceID.NotFound", "instance %q not found", vals[0])
		}
		insts[inst] = true
	}

	f := newFilter(req.Form)

	var resp ec2.InstancesResp
	resp.RequestId = reqId
	for _, r := range srv.reservations {
		var instances []ec2.Instance
		var groups []ec2.SecurityGroup
		for _, g := range r.groups {
			groups = append(groups, g.ec2SecurityGroup())
		}
		for _, inst := range r.instances {
			if len(insts) > 0 && !insts[inst] {
				continue
			}
			// make instances in state "shutting-down" to transition
			// to "terminated" first, so we can simulate: shutdown,
			// subsequent refresh of the state with Instances(),
			// terminated.
			if inst.state == ShuttingDown {
				inst.state = Terminated
			}

			ok, err := f.ok(inst)
			if ok {
				instance := inst.ec2instance()
				instance.SecurityGroups = groups
				instances = append(instances, instance)
			} else if err != nil {
				fatalf(400, "InvalidParameterValue", "describe instances: %v", err)
			}
		}
		if len(instances) > 0 {
			resp.Reservations = append(resp.Reservations, ec2.Reservation{
				ReservationId:  r.id,
				OwnerId:        ownerId,
				Instances:      instances,
				SecurityGroups: groups,
			})
		}
	}
	return &resp
}

func (srv *Server) describeSecurityGroups(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	// BUG similar bug to describeInstances, but for GroupName and GroupId
	srv.mu.Lock()
	defer srv.mu.Unlock()

	var groups []*securityGroup
	for name, vals := range req.Form {
		var g ec2.SecurityGroup
		switch {
		case strings.HasPrefix(name, "GroupName."):
			g.Name = vals[0]
		case strings.HasPrefix(name, "GroupId."):
			g.Id = vals[0]
		default:
			continue
		}
		sg := srv.group(g)
		if sg == nil {
			fatalf(400, "InvalidGroup.NotFound", "no such group %v", g)
		}
		groups = append(groups, sg)
	}
	if len(groups) == 0 {
		for _, g := range srv.groups {
			groups = append(groups, g)
		}
	}

	f := newFilter(req.Form)
	var resp ec2.SecurityGroupsResp
	resp.RequestId = reqId
	for _, group := range groups {
		ok, err := f.ok(group)
		if ok {
			resp.Groups = append(resp.Groups, ec2.SecurityGroupInfo{
				OwnerId:       ownerId,
				SecurityGroup: group.ec2SecurityGroup(),
				Description:   group.description,
				IPPerms:       group.ec2Perms(),
			})
		} else if err != nil {
			fatalf(400, "InvalidParameterValue", "describe security groups: %v", err)
		}
	}
	return &resp
}

func (srv *Server) authorizeSecurityGroupIngress(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	g := srv.group(ec2.SecurityGroup{
		Name: req.Form.Get("GroupName"),
		Id:   req.Form.Get("GroupId"),
	})
	if g == nil {
		fatalf(400, "InvalidGroup.NotFound", "group not found")
	}
	perms := srv.parsePerms(req)

	for _, p := range perms {
		if g.perms[p] {
			fatalf(400, "InvalidPermission.Duplicate", "Permission has already been authorized on the specified group")
		}
	}
	for _, p := range perms {
		g.perms[p] = true
	}
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "AuthorizeSecurityGroupIngressResponse"},
		RequestId: reqId,
	}
}

func (srv *Server) revokeSecurityGroupIngress(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	g := srv.group(ec2.SecurityGroup{
		Name: req.Form.Get("GroupName"),
		Id:   req.Form.Get("GroupId"),
	})
	if g == nil {
		fatalf(400, "InvalidGroup.NotFound", "group not found")
	}
	perms := srv.parsePerms(req)

	// Note EC2 does not give an error if asked to revoke an authorization
	// that does not exist.
	for _, p := range perms {
		delete(g.perms, p)
	}
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "RevokeSecurityGroupIngressResponse"},
		RequestId: reqId,
	}
}

var (
	secGroupPat = regexp.MustCompile(`^sg-[a-z0-9]+$`)
	cidrIpPat   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+/([0-9]+)$`)
	ownerIdPat  = regexp.MustCompile(`^[0-9]+$`)
)

// parsePerms returns a slice of permKey values extracted
// from the permission fields in req.
func (srv *Server) parsePerms(req *http.Request) []permKey {
	// perms maps an index found in the form to its associated
	// IPPerm. For instance, the form value with key
	// "IpPermissions.3.FromPort" will be stored in perms[3].FromPort
	perms := make(map[int]ec2.IPPerm)

	type subgroupKey struct {
		id1, id2 int
	}
	// Each IPPerm can have many source security groups.  The form key
	// for a source security group contains two indices: the index
	// of the IPPerm and the sub-index of the security group. The
	// sourceGroups map maps from a subgroupKey containing these
	// two indices to the associated security group. For instance,
	// the form value with key "IPPermissions.3.Groups.2.GroupName"
	// will be stored in sourceGroups[subgroupKey{3, 2}].Name.
	sourceGroups := make(map[subgroupKey]ec2.UserSecurityGroup)

	// For each value in the form we store its associated information in the
	// above maps. The maps are necessary because the form keys may
	// arrive in any order, and the indices are not
	// necessarily sequential or even small.
	for name, vals := range req.Form {
		val := vals[0]
		var id1 int
		var rest string
		if x, _ := fmt.Sscanf(name, "IpPermissions.%d.%s", &id1, &rest); x != 2 {
			continue
		}
		ec2p := perms[id1]
		switch {
		case rest == "FromPort":
			ec2p.FromPort = atoi(val)
		case rest == "ToPort":
			ec2p.ToPort = atoi(val)
		case rest == "IpProtocol":
			switch val {
			case "tcp", "udp", "icmp":
				ec2p.Protocol = val
			default:
				// check it's a well formed number
				atoi(val)
				ec2p.Protocol = val
			}
		case strings.HasPrefix(rest, "Groups."):
			k := subgroupKey{id1: id1}
			if x, _ := fmt.Sscanf(rest[len("Groups."):], "%d.%s", &k.id2, &rest); x != 2 {
				continue
			}
			g := sourceGroups[k]
			switch rest {
			case "UserId":
				// BUG if the user id is blank, this does not conform to the
				// way that EC2 handles it - a specified but blank owner id
				// can cause RevokeSecurityGroupIngress to fail with
				// "group not found" even if the security group id has been
				// correctly specified.
				// By failing here, we ensure that we fail early in this case.
				if !ownerIdPat.MatchString(val) {
					fatalf(400, "InvalidUserID.Malformed", "Invalid user ID: %q", val)
				}
				g.OwnerId = val
			case "GroupName":
				g.Name = val
			case "GroupId":
				if !secGroupPat.MatchString(val) {
					fatalf(400, "InvalidGroupId.Malformed", "Invalid group ID: %q", val)
				}
				g.Id = val
			default:
				fatalf(400, "UnknownParameter", "unknown parameter %q", name)
			}
			sourceGroups[k] = g
		case strings.HasPrefix(rest, "IpRanges."):
			var id2 int
			if x, _ := fmt.Sscanf(rest[len("IpRanges."):], "%d.%s", &id2, &rest); x != 2 {
				continue
			}
			switch rest {
			case "CidrIp":
				if !cidrIpPat.MatchString(val) {
					fatalf(400, "InvalidPermission.Malformed", "Invalid IP range: %q", val)
				}
				ec2p.SourceIPs = append(ec2p.SourceIPs, val)
			default:
				fatalf(400, "UnknownParameter", "unknown parameter %q", name)
			}
		default:
			fatalf(400, "UnknownParameter", "unknown parameter %q", name)
		}
		perms[id1] = ec2p
	}
	// Associate each set of source groups with its IPPerm.
	for k, g := range sourceGroups {
		p := perms[k.id1]
		p.SourceGroups = append(p.SourceGroups, g)
		perms[k.id1] = p
	}

	// Now that we have built up the IPPerms we need, we check for
	// parameter errors and build up a permKey for each permission,
	// looking up security groups from srv as we do so.
	var result []permKey
	for _, p := range perms {
		if p.FromPort > p.ToPort {
			fatalf(400, "InvalidParameterValue", "invalid port range")
		}
		k := permKey{
			protocol: p.Protocol,
			fromPort: p.FromPort,
			toPort:   p.ToPort,
		}
		for _, g := range p.SourceGroups {
			if g.OwnerId != "" && g.OwnerId != ownerId {
				fatalf(400, "InvalidGroup.NotFound", "group %q not found", g.Name)
			}
			var ec2g ec2.SecurityGroup
			switch {
			case g.Id != "":
				ec2g.Id = g.Id
			case g.Name != "":
				ec2g.Name = g.Name
			}
			k.group = srv.group(ec2g)
			if k.group == nil {
				fatalf(400, "InvalidGroup.NotFound", "group %v not found", g)
			}
			result = append(result, k)
		}
		k.group = nil
		for _, ip := range p.SourceIPs {
			k.ipAddr = ip
			result = append(result, k)
		}
	}
	return result
}

func (srv *Server) deleteSecurityGroup(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	g := srv.group(ec2.SecurityGroup{
		Name: req.Form.Get("GroupName"),
		Id:   req.Form.Get("GroupId"),
	})
	if g == nil {
		fatalf(400, "InvalidGroup.NotFound", "group not found")
	}
	for _, r := range srv.reservations {
		for _, h := range r.groups {
			if h == g && r.hasRunningMachine() {
				fatalf(500, "InvalidGroup.InUse", "group is currently in use by a running instance")
			}
		}
	}
	for _, sg := range srv.groups {
		// If a group refers to itself, it's ok to delete it.
		if sg == g {
			continue
		}
		for k := range sg.perms {
			if k.group == g {
				fatalf(500, "InvalidGroup.InUse", "group is currently in use by group %q", sg.id)
			}
		}
	}

	delete(srv.groups, g.id)
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "DeleteSecurityGroupResponse"},
		RequestId: reqId,
	}
}

func (srv *Server) createVpc(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	cidrBlock := parseCidr(req.Form.Get("CidrBlock"))
	tenancy := req.Form.Get("InstanceTenancy")
	if tenancy == "" {
		tenancy = "default"
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	v := &vpc{ec2.VPC{
		Id:              fmt.Sprintf("vpc-%d", srv.vpcId.next()),
		State:           "available",
		CIDRBlock:       cidrBlock,
		DHCPOptionsId:   fmt.Sprintf("dopt-%d", srv.dhcpOptsId.next()),
		InstanceTenancy: tenancy,
	}}
	srv.vpcs[v.Id] = v
	r := &ec2.CreateVPCResp{
		RequestId: reqId,
		VPC:       v.VPC,
	}
	return r
}

func (srv *Server) deleteVpc(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	v := srv.vpc(req.Form.Get("VpcId"))
	srv.mu.Lock()
	defer srv.mu.Unlock()

	delete(srv.vpcs, v.Id)
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "DeleteVpcResponse"},
		RequestId: reqId,
	}
}

func (srv *Server) describeVpcs(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	idMap := collectIds(req.Form, "VpcId.")
	f := newFilter(req.Form)
	var resp ec2.VPCsResp
	resp.RequestId = reqId
	for _, v := range srv.vpcs {
		ok, err := f.ok(v)
		if ok && (len(idMap) == 0 || idMap[v.Id]) {
			resp.VPCs = append(resp.VPCs, v.VPC)
		} else if err != nil {
			fatalf(400, "InvalidParameterValue", "describe VPCs: %v", err)
		}
	}
	return &resp
}

func (srv *Server) createSubnet(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	v := srv.vpc(req.Form.Get("VpcId"))
	cidrBlock := parseCidr(req.Form.Get("CidrBlock"))
	availZone := req.Form.Get("AvailabilityZone")
	if availZone == "" {
		// Assign one automatically as AWS does.
		availZone = "us-east-1b"
	}
	// calculate the available IP addresses, removing the first 4 and
	// the last, which are reserved by AWS. Since we already checked
	// the CIDR is valid, we don't check the error here.
	_, ipnet, _ := net.ParseCIDR(cidrBlock)
	maskOnes, maskBits := ipnet.Mask.Size()
	availIPs := 1<<uint(maskBits-maskOnes) - 5

	srv.mu.Lock()
	defer srv.mu.Unlock()
	s := &subnet{ec2.Subnet{
		Id:               fmt.Sprintf("subnet-%d", srv.subnetId.next()),
		VPCId:            v.Id,
		State:            "available",
		CIDRBlock:        cidrBlock,
		AvailZone:        availZone,
		AvailableIPCount: availIPs,
	}}
	srv.subnets[s.Id] = s
	r := &ec2.CreateSubnetResp{
		RequestId: reqId,
		Subnet:    s.Subnet,
	}
	return r
}

func (srv *Server) deleteSubnet(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	s := srv.subnet(req.Form.Get("SubnetId"))
	srv.mu.Lock()
	defer srv.mu.Unlock()

	delete(srv.subnets, s.Id)
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "DeleteSubnetResponse"},
		RequestId: reqId,
	}
}

func (srv *Server) describeSubnets(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	idMap := collectIds(req.Form, "SubnetId.")
	f := newFilter(req.Form)
	var resp ec2.SubnetsResp
	resp.RequestId = reqId
	for _, s := range srv.subnets {
		ok, err := f.ok(s)
		if ok && (len(idMap) == 0 || idMap[s.Id]) {
			resp.Subnets = append(resp.Subnets, s.Subnet)
		} else if err != nil {
			fatalf(400, "InvalidParameterValue", "describe subnets: %v", err)
		}
	}
	return &resp
}

func (srv *Server) createIFace(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	s := srv.subnet(req.Form.Get("SubnetId"))
	ipMap := make(map[int]ec2.PrivateIP)
	primaryIP := req.Form.Get("PrivateIpAddress")
	if primaryIP != "" {
		ipMap[0] = ec2.PrivateIP{Address: primaryIP, IsPrimary: true}
	}
	desc := req.Form.Get("Description")

	var groups []ec2.SecurityGroup
	for name, vals := range req.Form {
		if strings.HasPrefix(name, "SecurityGroupId.") {
			g := ec2.SecurityGroup{Id: vals[0]}
			sg := srv.group(g)
			if sg == nil {
				fatalf(400, "InvalidGroup.NotFound", "no such group %v", g)
			}
			groups = append(groups, sg.ec2SecurityGroup())
		}
		if strings.HasPrefix(name, "PrivateIpAddresses.") {
			var ip ec2.PrivateIP
			parts := strings.Split(name, ".")
			index := atoi(parts[1]) - 1
			if index < 0 {
				fatalf(400, "InvalidParameterValue", "invalid index %s", name)
			}
			if _, ok := ipMap[index]; ok {
				ip = ipMap[index]
			}
			switch parts[2] {
			case "PrivateIpAddress":
				ip.Address = vals[0]
			case "Primary":
				val, err := strconv.ParseBool(vals[0])
				if err != nil {
					fatalf(400, "InvalidParameterValue", "bad flag %s: %s", name, vals[0])
				}
				ip.IsPrimary = val
			}
			ipMap[index] = ip
		}
	}
	privateIPs := make([]ec2.PrivateIP, len(ipMap))
	for index, ip := range ipMap {
		if ip.IsPrimary {
			primaryIP = ip.Address
		}
		privateIPs[index] = ip
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	i := &iface{ec2.NetworkInterface{
		Id:               fmt.Sprintf("eni-%d", srv.ifaceId.next()),
		SubnetId:         s.Id,
		VPCId:            s.VPCId,
		AvailZone:        s.AvailZone,
		Description:      desc,
		OwnerId:          ownerId,
		Status:           "available",
		MACAddress:       fmt.Sprintf("%02d:81:60:cb:27:37", srv.ifaceId),
		PrivateIPAddress: primaryIP,
		SourceDestCheck:  true,
		Groups:           groups,
		PrivateIPs:       privateIPs,
	}}
	srv.ifaces[i.Id] = i
	r := &ec2.CreateNetworkInterfaceResp{
		RequestId:        reqId,
		NetworkInterface: i.NetworkInterface,
	}
	return r
}

func (srv *Server) deleteIFace(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	i := srv.iface(req.Form.Get("NetworkInterfaceId"))

	srv.mu.Lock()
	defer srv.mu.Unlock()

	delete(srv.ifaces, i.Id)
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "DeleteNetworkInterface"},
		RequestId: reqId,
	}
}

func (srv *Server) describeIFaces(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	idMap := collectIds(req.Form, "NetworkInterfaceId.")
	f := newFilter(req.Form)
	var resp ec2.NetworkInterfacesResp
	resp.RequestId = reqId
	for _, i := range srv.ifaces {
		ok, err := f.ok(i)
		if ok && (len(idMap) == 0 || idMap[i.Id]) {
			resp.Interfaces = append(resp.Interfaces, i.NetworkInterface)
		} else if err != nil {
			fatalf(400, "InvalidParameterValue", "describe ifaces: %v", err)
		}
	}
	return &resp
}

func (srv *Server) attachIFace(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	i := srv.iface(req.Form.Get("NetworkInterfaceId"))
	inst := srv.instance(req.Form.Get("InstanceId"))
	devIndex := atoi(req.Form.Get("DeviceIndex"))

	srv.mu.Lock()
	defer srv.mu.Unlock()
	a := &attachment{ec2.NetworkInterfaceAttachment{
		Id:                  fmt.Sprintf("eni-attach-%d", srv.attachId.next()),
		InstanceId:          inst.id(),
		InstanceOwnerId:     ownerId,
		DeviceIndex:         devIndex,
		Status:              "in-use",
		AttachTime:          time.Now().Format(time.RFC3339),
		DeleteOnTermination: true,
	}}
	srv.attachments[a.Id] = a
	i.Attachment = a.NetworkInterfaceAttachment
	srv.ifaces[i.Id] = i
	r := &ec2.AttachNetworkInterfaceResp{
		RequestId:    reqId,
		AttachmentId: a.Id,
	}
	return r
}

func (srv *Server) detachIFace(w http.ResponseWriter, req *http.Request, reqId string) interface{} {
	att := srv.attachment(req.Form.Get("AttachmentId"))

	srv.mu.Lock()
	defer srv.mu.Unlock()

	for _, i := range srv.ifaces {
		if i.Attachment.Id == att.Id {
			i.Attachment = ec2.NetworkInterfaceAttachment{}
			srv.ifaces[i.Id] = i
			break
		}
	}
	delete(srv.attachments, att.Id)
	return &ec2.SimpleResp{
		XMLName:   xml.Name{"", "DetachNetworkInterface"},
		RequestId: reqId,
	}
}

func (r *reservation) hasRunningMachine() bool {
	for _, inst := range r.instances {
		if inst.state.Code != ShuttingDown.Code && inst.state.Code != Terminated.Code {
			return true
		}
	}
	return false
}

func parseCidr(val string) string {
	if val == "" {
		fatalf(400, "MissingParameter", "missing cidrBlock")
	}
	if _, _, err := net.ParseCIDR(val); err != nil {
		fatalf(400, "InvalidParameterValue", "bad CIDR %q: %v", val, err)
	}
	return val
}

func (srv *Server) vpc(id string) *vpc {
	if id == "" {
		fatalf(400, "MissingParameter", "missing vpcId")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	v, found := srv.vpcs[id]
	if !found {
		fatalf(400, "InvalidVpcID.NotFound", "VPC %s not found", id)
	}
	return v
}

func (srv *Server) subnet(id string) *subnet {
	if id == "" {
		fatalf(400, "MissingParameter", "missing subnetId")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	s, found := srv.subnets[id]
	if !found {
		fatalf(400, "InvalidSubnetID.NotFound", "subnet %s not found", id)
	}
	return s
}

func (srv *Server) iface(id string) *iface {
	if id == "" {
		fatalf(400, "MissingParameter", "missing networkInterfaceId")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	i, found := srv.ifaces[id]
	if !found {
		fatalf(400, "InvalidNetworkInterfaceID.NotFound", "interface %s not found", id)
	}
	return i
}

func (srv *Server) instance(id string) *Instance {
	if id == "" {
		fatalf(400, "MissingParameter", "missing instanceId")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	inst, found := srv.instances[id]
	if !found {
		fatalf(400, "InvalidInstanceID.NotFound", "instance %s not found", id)
	}
	return inst
}

func (srv *Server) attachment(id string) *attachment {
	if id == "" {
		fatalf(400, "MissingParameter", "missing attachmentId")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	att, found := srv.attachments[id]
	if !found {
		fatalf(
			400,
			"InvalidNetworkInterfaceAttachmentId.NotFound",
			"attachment %s not found", id,
		)
	}
	return att
}

// collectIds takes all values with the given prefix from form and
// returns a map with the ids as keys.
func collectIds(form url.Values, prefix string) map[string]bool {
	idMap := make(map[string]bool)
	for name, vals := range form {
		if strings.HasPrefix(name, prefix) {
			idMap[vals[0]] = true
		}
	}
	return idMap
}

type counter int

func (c *counter) next() (i int) {
	i = int(*c)
	(*c)++
	return
}

// atoi is like strconv.Atoi but is fatal if the
// string is not well formed.
func atoi(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		fatalf(400, "InvalidParameterValue", "bad number: %v", err)
	}
	return i
}

func fatalf(statusCode int, code string, f string, a ...interface{}) {
	panic(&ec2.Error{
		StatusCode: statusCode,
		Code:       code,
		Message:    fmt.Sprintf(f, a...),
	})
}
