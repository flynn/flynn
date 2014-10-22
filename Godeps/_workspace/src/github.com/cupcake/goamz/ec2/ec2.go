//
// goamz - Go packages to interact with the Amazon Web Services.
//
//   https://wiki.ubuntu.com/goamz
//
// Copyright (c) 2011 Canonical Ltd.
//

package ec2

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
)

const (
	debug = false

	// legacyAPIVersion is the AWS API version used for all but
	// VPC-related requests.
	legacyAPIVersion = "2011-12-15"

	// AWS API version used for VPC-related calls.
	vpcAPIVersion = "2013-10-15"
)

// The EC2 type encapsulates operations with a specific EC2 region.
type EC2 struct {
	aws.Auth
	aws.Region
	private byte // Reserve the right of using private data.
}

// New creates a new EC2.
func New(auth aws.Auth, region aws.Region) *EC2 {
	return &EC2{auth, region, 0}
}

// ----------------------------------------------------------------------------
// Filtering helper.

// Filter builds filtering parameters to be used in an EC2 query which supports
// filtering.  For example:
//
//     filter := NewFilter()
//     filter.Add("architecture", "i386")
//     filter.Add("launch-index", "0")
//     resp, err := ec2.Instances(nil, filter)
//
type Filter struct {
	m map[string][]string
}

// NewFilter creates a new Filter.
func NewFilter() *Filter {
	return &Filter{make(map[string][]string)}
}

// Add appends a filtering parameter with the given name and value(s).
func (f *Filter) Add(name string, value ...string) {
	f.m[name] = append(f.m[name], value...)
}

func (f *Filter) addParams(params map[string]string) {
	if f != nil {
		a := make([]string, len(f.m))
		i := 0
		for k := range f.m {
			a[i] = k
			i++
		}
		sort.StringSlice(a).Sort()
		for i, k := range a {
			prefix := "Filter." + strconv.Itoa(i+1)
			params[prefix+".Name"] = k
			for j, v := range f.m[k] {
				params[prefix+".Value."+strconv.Itoa(j+1)] = v
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Request dispatching logic.

// Error encapsulates an error returned by EC2.
//
// See http://goo.gl/VZGuC for more details.
type Error struct {
	// HTTP status code (200, 403, ...)
	StatusCode int
	// EC2 error code ("UnsupportedOperation", ...)
	Code string
	// The human-oriented error message
	Message   string
	RequestId string `xml:"RequestID"`
}

func (err *Error) Error() string {
	if err.Code == "" {
		return err.Message
	}

	return fmt.Sprintf("%s (%s)", err.Message, err.Code)
}

// For now a single error inst is being exposed. In the future it may be useful
// to provide access to all of them, but rather than doing it as an array/slice,
// use a *next pointer, so that it's backward compatible and it continues to be
// easy to handle the first error, which is what most people will want.
type xmlErrors struct {
	RequestId string  `xml:"RequestID"`
	Errors    []Error `xml:"Errors>Error"`
}

var timeNow = time.Now

func (ec2 *EC2) query(params map[string]string, resp interface{}) error {
	params["Timestamp"] = timeNow().In(time.UTC).Format(time.RFC3339)
	endpoint, err := url.Parse(ec2.Region.EC2Endpoint)
	if err != nil {
		return err
	}
	if endpoint.Path == "" {
		endpoint.Path = "/"
	}
	sign(ec2.Auth, "GET", endpoint.Path, params, endpoint.Host)
	endpoint.RawQuery = multimap(params).Encode()
	if debug {
		log.Printf("get { %v } -> {\n", endpoint.String())
	}
	r, err := http.Get(endpoint.String())
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if debug {
		dump, _ := httputil.DumpResponse(r, true)
		log.Printf("response:\n")
		log.Printf("%v\n}\n", string(dump))
	}
	if r.StatusCode != 200 {
		return buildError(r)
	}
	err = xml.NewDecoder(r.Body).Decode(resp)
	return err
}

func multimap(p map[string]string) url.Values {
	q := make(url.Values, len(p))
	for k, v := range p {
		q[k] = []string{v}
	}
	return q
}

func buildError(r *http.Response) error {
	errors := xmlErrors{}
	xml.NewDecoder(r.Body).Decode(&errors)
	var err Error
	if len(errors.Errors) > 0 {
		err = errors.Errors[0]
	}
	err.RequestId = errors.RequestId
	err.StatusCode = r.StatusCode
	if err.Message == "" {
		err.Message = r.Status
	}
	return &err
}

func makeParams(action string) map[string]string {
	return makeParamsWithVersion(action, legacyAPIVersion)
}

func makeParamsVPC(action string) map[string]string {
	return makeParamsWithVersion(action, vpcAPIVersion)
}

func makeParamsWithVersion(action, version string) map[string]string {
	params := make(map[string]string)
	params["Action"] = action
	params["Version"] = version
	return params
}

func addParamsList(params map[string]string, label string, ids []string) {
	for i, id := range ids {
		params[label+"."+strconv.Itoa(i+1)] = id
	}
}

// ----------------------------------------------------------------------------
// Instance management functions and types.

// RunNetworkInterface encapsulates options for a single network
// interface, specified when calling RunInstances.
//
// If Id is set, it must match an existing VPC network interface, and
// in this case only a single instance can be launched. If Id is not
// set, a new network interface will be created for each instance.
//
// The following fields are required when creating a new network
// interface (i.e. Id is empty): DeviceIndex, SubnetId, Description
// (only used if set), SecurityGroupIds.
//
// PrivateIPs can be used to add one or more private IP addresses to a
// network interface. Only one of the IP addresses can be set as
// primary. If none are given, EC2 selects a primary IP for each
// created interface from the subnet pool.
//
// When SecondaryPrivateIPCount is non-zero, EC2 allocates that number
// of IP addresses from within the subnet range and sets them as
// secondary IPs. The number of IP addresses that can be assigned to a
// network interface varies by instance type.
type RunNetworkInterface struct {
	Id                       string
	DeviceIndex              int
	SubnetId                 string
	Description              string
	PrivateIPs               []PrivateIP
	SecurityGroupIds         []string
	DeleteOnTermination      bool
	SecondaryPrivateIPCount  int
	AssociatePublicIPAddress bool
}

// The RunInstances type encapsulates options for the respective request in EC2.
//
// See http://goo.gl/Mcm3b for more details.
type RunInstances struct {
	ImageId               string
	MinCount              int
	MaxCount              int
	KeyName               string
	InstanceType          string
	SecurityGroups        []SecurityGroup
	KernelId              string
	RamdiskId             string
	UserData              []byte
	AvailZone             string
	PlacementGroupName    string
	Monitoring            bool
	SubnetId              string
	DisableAPITermination bool
	ShutdownBehavior      string
	PrivateIPAddress      string
	BlockDeviceMappings   []BlockDeviceMapping
	NetworkInterfaces     []RunNetworkInterface
}

// Response to a RunInstances request.
//
// See http://goo.gl/Mcm3b for more details.
type RunInstancesResp struct {
	RequestId      string          `xml:"requestId"`
	ReservationId  string          `xml:"reservationId"`
	OwnerId        string          `xml:"ownerId"`
	SecurityGroups []SecurityGroup `xml:"groupSet>item"`
	Instances      []Instance      `xml:"instancesSet>item"`
}

// Instance encapsulates a running instance in EC2.
//
// See http://goo.gl/OCH8a for more details.
type Instance struct {
	InstanceId         string             `xml:"instanceId"`
	InstanceType       string             `xml:"instanceType"`
	ImageId            string             `xml:"imageId"`
	PrivateDNSName     string             `xml:"privateDnsName"`
	DNSName            string             `xml:"dnsName"`
	IPAddress          string             `xml:"ipAddress"`
	PrivateIPAddress   string             `xml:"privateIpAddress"`
	SubnetId           string             `xml:"subnetId"`
	VPCId              string             `xml:"vpcId"`
	SourceDestCheck    bool               `xml:"sourceDestCheck"`
	KeyName            string             `xml:"keyName"`
	AMILaunchIndex     int                `xml:"amiLaunchIndex"`
	Hypervisor         string             `xml:"hypervisor"`
	VirtType           string             `xml:"virtualizationType"`
	Monitoring         string             `xml:"monitoring>state"`
	AvailZone          string             `xml:"placement>availabilityZone"`
	PlacementGroupName string             `xml:"placement>groupName"`
	State              InstanceState      `xml:"instanceState"`
	Tags               []Tag              `xml:"tagSet>item"`
	SecurityGroups     []SecurityGroup    `xml:"groupSet>item"`
	NetworkInterfaces  []NetworkInterface `xml:"networkInterfaceSet>item"`
}

// RunInstances starts new instances in EC2.
// If options.MinCount and options.MaxCount are both zero, a single instance
// will be started; otherwise if options.MaxCount is zero, options.MinCount
// will be used instead.
//
// See http://goo.gl/Mcm3b for more details.
func (ec2 *EC2) RunInstances(options *RunInstances) (resp *RunInstancesResp, err error) {
	params := prepareRunParams(*options)
	params["ImageId"] = options.ImageId
	params["InstanceType"] = options.InstanceType
	var min, max int
	if options.MinCount == 0 && options.MaxCount == 0 {
		min = 1
		max = 1
	} else if options.MaxCount == 0 {
		min = options.MinCount
		max = min
	} else {
		min = options.MinCount
		max = options.MaxCount
	}
	params["MinCount"] = strconv.Itoa(min)
	params["MaxCount"] = strconv.Itoa(max)
	i, j := 1, 1
	for _, g := range options.SecurityGroups {
		if g.Id != "" {
			params["SecurityGroupId."+strconv.Itoa(i)] = g.Id
			i++
		} else {
			params["SecurityGroup."+strconv.Itoa(j)] = g.Name
			j++
		}
	}
	prepareBlockDevices(params, options.BlockDeviceMappings)
	prepareNetworkInterfaces(params, options.NetworkInterfaces)
	token, err := clientToken()
	if err != nil {
		return nil, err
	}
	params["ClientToken"] = token

	if options.KeyName != "" {
		params["KeyName"] = options.KeyName
	}
	if options.KernelId != "" {
		params["KernelId"] = options.KernelId
	}
	if options.RamdiskId != "" {
		params["RamdiskId"] = options.RamdiskId
	}
	if options.UserData != nil {
		userData := make([]byte, b64.EncodedLen(len(options.UserData)))
		b64.Encode(userData, options.UserData)
		params["UserData"] = string(userData)
	}
	if options.AvailZone != "" {
		params["Placement.AvailabilityZone"] = options.AvailZone
	}
	if options.PlacementGroupName != "" {
		params["Placement.GroupName"] = options.PlacementGroupName
	}
	if options.Monitoring {
		params["Monitoring.Enabled"] = "true"
	}
	if options.SubnetId != "" {
		params["SubnetId"] = options.SubnetId
	}
	if options.DisableAPITermination {
		params["DisableApiTermination"] = "true"
	}
	if options.ShutdownBehavior != "" {
		params["InstanceInitiatedShutdownBehavior"] = options.ShutdownBehavior
	}
	if options.PrivateIPAddress != "" {
		params["PrivateIpAddress"] = options.PrivateIPAddress
	}

	resp = &RunInstancesResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

func prepareRunParams(options RunInstances) map[string]string {
	if options.SubnetId != "" || len(options.NetworkInterfaces) > 0 {
		// When either SubnetId or NetworkInterfaces are specified, we
		// need to use the API version with complete VPC support.
		return makeParamsVPC("RunInstances")
	} else {
		return makeParams("RunInstances")
	}
}

func prepareBlockDevices(params map[string]string, blockDevs []BlockDeviceMapping) {
	for i, b := range blockDevs {
		n := strconv.Itoa(i + 1)
		prefix := "BlockDeviceMapping." + n
		if b.DeviceName != "" {
			params[prefix+".DeviceName"] = b.DeviceName
		}
		if b.VirtualName != "" {
			params[prefix+".VirtualName"] = b.VirtualName
		}
		if b.SnapshotId != "" {
			params[prefix+".Ebs.SnapshotId"] = b.SnapshotId
		}
		if b.VolumeType != "" {
			params[prefix+".Ebs.VolumeType"] = b.VolumeType
		}
		if b.VolumeSize > 0 {
			params[prefix+".Ebs.VolumeSize"] = strconv.FormatInt(b.VolumeSize, 10)
		}
		if b.IOPS > 0 {
			params[prefix+".Ebs.Iops"] = strconv.FormatInt(b.IOPS, 10)
		}
		if b.DeleteOnTermination {
			params[prefix+".Ebs.DeleteOnTermination"] = "true"
		}
	}
}

func prepareNetworkInterfaces(params map[string]string, nics []RunNetworkInterface) {
	for i, ni := range nics {
		// Unlike other lists, NetworkInterface and PrivateIpAddresses
		// should start from 0, not 1, according to the examples
		// requests in the API documentation here http://goo.gl/Mcm3b.
		n := strconv.Itoa(i)
		prefix := "NetworkInterface." + n
		if ni.Id != "" {
			params[prefix+".NetworkInterfaceId"] = ni.Id
		}
		params[prefix+".DeviceIndex"] = strconv.Itoa(ni.DeviceIndex)
		if ni.SubnetId != "" {
			params[prefix+".SubnetId"] = ni.SubnetId
		}
		if ni.Description != "" {
			params[prefix+".Description"] = ni.Description
		}
		for j, gid := range ni.SecurityGroupIds {
			k := strconv.Itoa(j + 1)
			params[prefix+".SecurityGroupId."+k] = gid
		}
		if ni.DeleteOnTermination {
			params[prefix+".DeleteOnTermination"] = "true"
		}
		if ni.SecondaryPrivateIPCount > 0 {
			val := strconv.Itoa(ni.SecondaryPrivateIPCount)
			params[prefix+".SecondaryPrivateIpAddressCount"] = val
		}
		params[prefix+".AssociatePublicIpAddress"] = fmt.Sprintf("%t", ni.AssociatePublicIPAddress)
		for j, ip := range ni.PrivateIPs {
			k := strconv.Itoa(j)
			subprefix := prefix + ".PrivateIpAddresses." + k
			params[subprefix+".PrivateIpAddress"] = ip.Address
			params[subprefix+".Primary"] = strconv.FormatBool(ip.IsPrimary)
		}
	}
}

func clientToken() (string, error) {
	// Maximum EC2 client token size is 64 bytes.
	// Each byte expands to two when hex encoded.
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Response to a TerminateInstances request.
//
// See http://goo.gl/3BKHj for more details.
type TerminateInstancesResp struct {
	RequestId    string                `xml:"requestId"`
	StateChanges []InstanceStateChange `xml:"instancesSet>item"`
}

// InstanceState encapsulates the state of an instance in EC2.
//
// See http://goo.gl/y3ZBq for more details.
type InstanceState struct {
	Code int    `xml:"code"` // Watch out, bits 15-8 have unpublished meaning.
	Name string `xml:"name"`
}

// InstanceStateChange informs of the previous and current states
// for an instance when a state change is requested.
type InstanceStateChange struct {
	InstanceId    string        `xml:"instanceId"`
	CurrentState  InstanceState `xml:"currentState"`
	PreviousState InstanceState `xml:"previousState"`
}

// TerminateInstances requests the termination of instances when the given ids.
//
// See http://goo.gl/3BKHj for more details.
func (ec2 *EC2) TerminateInstances(instIds []string) (resp *TerminateInstancesResp, err error) {
	params := makeParams("TerminateInstances")
	addParamsList(params, "InstanceId", instIds)
	resp = &TerminateInstancesResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// Response to a DescribeInstances request.
//
// See http://goo.gl/mLbmw for more details.
type InstancesResp struct {
	RequestId    string        `xml:"requestId"`
	Reservations []Reservation `xml:"reservationSet>item"`
}

// Reservation represents details about a reservation in EC2.
//
// See http://goo.gl/0ItPT for more details.
type Reservation struct {
	ReservationId  string          `xml:"reservationId"`
	OwnerId        string          `xml:"ownerId"`
	RequesterId    string          `xml:"requesterId"`
	SecurityGroups []SecurityGroup `xml:"groupSet>item"`
	Instances      []Instance      `xml:"instancesSet>item"`
}

// Instances returns details about instances in EC2.  Both parameters
// are optional, and if provided will limit the instances returned to those
// matching the given instance ids or filtering rules.
//
// See http://goo.gl/4No7c for more details.
func (ec2 *EC2) Instances(instIds []string, filter *Filter) (resp *InstancesResp, err error) {
	params := makeParams("DescribeInstances")
	addParamsList(params, "InstanceId", instIds)
	filter.addParams(params)
	resp = &InstancesResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// ----------------------------------------------------------------------------
// Image and snapshot management functions and types.

// Response to a DescribeImages request.
//
// See http://goo.gl/hLnyg for more details.
type ImagesResp struct {
	RequestId string  `xml:"requestId"`
	Images    []Image `xml:"imagesSet>item"`
}

// BlockDeviceMapping represents the association of a block device with an image.
//
// See http://goo.gl/wnDBf for more details.
type BlockDeviceMapping struct {
	DeviceName          string `xml:"deviceName"`
	VirtualName         string `xml:"virtualName"`
	SnapshotId          string `xml:"ebs>snapshotId"`
	VolumeType          string `xml:"ebs>volumeType"`
	VolumeSize          int64  `xml:"ebs>volumeSize"` // Size is given in GB
	DeleteOnTermination bool   `xml:"ebs>deleteOnTermination"`

	// The number of I/O operations per second (IOPS) that the volume supports.
	IOPS int64 `xml:"ebs>iops"`
}

// Image represents details about an image.
//
// See http://goo.gl/iSqJG for more details.
type Image struct {
	Id                 string               `xml:"imageId"`
	Name               string               `xml:"name"`
	Description        string               `xml:"description"`
	Type               string               `xml:"imageType"`
	State              string               `xml:"imageState"`
	Location           string               `xml:"imageLocation"`
	Public             bool                 `xml:"isPublic"`
	Architecture       string               `xml:"architecture"`
	Platform           string               `xml:"platform"`
	ProductCodes       []string             `xml:"productCode>item>productCode"`
	KernelId           string               `xml:"kernelId"`
	RamdiskId          string               `xml:"ramdiskId"`
	StateReason        string               `xml:"stateReason"`
	OwnerId            string               `xml:"imageOwnerId"`
	OwnerAlias         string               `xml:"imageOwnerAlias"`
	RootDeviceType     string               `xml:"rootDeviceType"`
	RootDeviceName     string               `xml:"rootDeviceName"`
	VirtualizationType string               `xml:"virtualizationType"`
	Hypervisor         string               `xml:"hypervisor"`
	BlockDevices       []BlockDeviceMapping `xml:"blockDeviceMapping>item"`
}

// Images returns details about available images.
// The ids and filter parameters, if provided, will limit the images returned.
// For example, to get all the private images associated with this account set
// the boolean filter "is-private" to true.
//
// Note: calling this function with nil ids and filter parameters will result in
// a very large number of images being returned.
//
// See http://goo.gl/SRBhW for more details.
func (ec2 *EC2) Images(ids []string, filter *Filter) (resp *ImagesResp, err error) {
	params := makeParams("DescribeImages")
	for i, id := range ids {
		params["ImageId."+strconv.Itoa(i+1)] = id
	}
	filter.addParams(params)

	resp = &ImagesResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// Response to a CreateSnapshot request.
//
// See http://goo.gl/ttcda for more details.
type CreateSnapshotResp struct {
	RequestId string `xml:"requestId"`
	Snapshot
}

// CreateSnapshot creates a volume snapshot and stores it in S3.
//
// See http://goo.gl/ttcda for more details.
func (ec2 *EC2) CreateSnapshot(volumeId, description string) (resp *CreateSnapshotResp, err error) {
	params := makeParams("CreateSnapshot")
	params["VolumeId"] = volumeId
	params["Description"] = description

	resp = &CreateSnapshotResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// DeleteSnapshots deletes the volume snapshots with the given ids.
//
// Note: If you make periodic snapshots of a volume, the snapshots are
// incremental so that only the blocks on the device that have changed
// since your last snapshot are incrementally saved in the new snapshot.
// Even though snapshots are saved incrementally, the snapshot deletion
// process is designed so that you need to retain only the most recent
// snapshot in order to restore the volume.
//
// See http://goo.gl/vwU1y for more details.
func (ec2 *EC2) DeleteSnapshots(ids []string) (resp *SimpleResp, err error) {
	params := makeParams("DeleteSnapshot")
	for i, id := range ids {
		params["SnapshotId."+strconv.Itoa(i+1)] = id
	}

	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// Response to a DescribeSnapshots request.
//
// See http://goo.gl/nClDT for more details.
type SnapshotsResp struct {
	RequestId string     `xml:"requestId"`
	Snapshots []Snapshot `xml:"snapshotSet>item"`
}

// Snapshot represents details about a volume snapshot.
//
// See http://goo.gl/nkovs for more details.
type Snapshot struct {
	Id          string `xml:"snapshotId"`
	VolumeId    string `xml:"volumeId"`
	VolumeSize  string `xml:"volumeSize"`
	Status      string `xml:"status"`
	StartTime   string `xml:"startTime"`
	Description string `xml:"description"`
	Progress    string `xml:"progress"`
	OwnerId     string `xml:"ownerId"`
	OwnerAlias  string `xml:"ownerAlias"`
	Tags        []Tag  `xml:"tagSet>item"`
}

// Snapshots returns details about volume snapshots available to the user.
// The ids and filter parameters, if provided, limit the snapshots returned.
//
// See http://goo.gl/ogJL4 for more details.
func (ec2 *EC2) Snapshots(ids []string, filter *Filter) (resp *SnapshotsResp, err error) {
	params := makeParams("DescribeSnapshots")
	for i, id := range ids {
		params["SnapshotId."+strconv.Itoa(i+1)] = id
	}
	filter.addParams(params)

	resp = &SnapshotsResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return
}

// ----------------------------------------------------------------------------
// Security group management functions and types.

// SimpleResp represents a response to an EC2 request which on success will
// return no other information besides a request id.
type SimpleResp struct {
	XMLName   xml.Name
	RequestId string `xml:"requestId"`
}

// CreateSecurityGroupResp represents a response to a CreateSecurityGroup request.
type CreateSecurityGroupResp struct {
	SecurityGroup
	RequestId string `xml:"requestId"`
}

// CreateSecurityGroup creates a security group with the provided name
// and description.
//
// See http://goo.gl/Eo7Yl for more details.
func (ec2 *EC2) CreateSecurityGroup(name, description string) (resp *CreateSecurityGroupResp, err error) {
	return ec2.CreateSecurityGroupVPC("", name, description)
}

// CreateSecurityGroupVPC creates a security group in EC2, associated
// with the given VPC ID. If vpcId is empty, this call is equivalent
// to CreateSecurityGroup.
//
// See http://goo.gl/Eo7Yl for more details.
func (ec2 *EC2) CreateSecurityGroupVPC(vpcId, name, description string) (resp *CreateSecurityGroupResp, err error) {
	params := makeParamsVPC("CreateSecurityGroup")
	params["GroupName"] = name
	params["GroupDescription"] = description
	if vpcId != "" {
		params["VpcId"] = vpcId
	}

	resp = &CreateSecurityGroupResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	resp.Name = name
	return resp, nil
}

// SecurityGroupsResp represents a response to a DescribeSecurityGroups
// request in EC2.
//
// See http://goo.gl/k12Uy for more details.
type SecurityGroupsResp struct {
	RequestId string              `xml:"requestId"`
	Groups    []SecurityGroupInfo `xml:"securityGroupInfo>item"`
}

// SecurityGroup encapsulates details for a security group in EC2.
//
// See http://goo.gl/CIdyP for more details.
type SecurityGroupInfo struct {
	SecurityGroup
	VPCId       string   `xml:"vpcId"`
	OwnerId     string   `xml:"ownerId"`
	Description string   `xml:"groupDescription"`
	IPPerms     []IPPerm `xml:"ipPermissions>item"`
}

// IPPerm represents an allowance within an EC2 security group.
//
// See http://goo.gl/4oTxv for more details.
type IPPerm struct {
	Protocol     string              `xml:"ipProtocol"`
	FromPort     int                 `xml:"fromPort"`
	ToPort       int                 `xml:"toPort"`
	SourceIPs    []string            `xml:"ipRanges>item>cidrIp"`
	SourceGroups []UserSecurityGroup `xml:"groups>item"`
}

// UserSecurityGroup holds a security group and the owner
// of that group.
type UserSecurityGroup struct {
	Id      string `xml:"groupId"`
	Name    string `xml:"groupName"`
	OwnerId string `xml:"userId"`
}

// SecurityGroup represents an EC2 security group.
// If SecurityGroup is used as a parameter, then one of Id or Name
// may be empty. If both are set, then Id is used.
type SecurityGroup struct {
	Id   string `xml:"groupId"`
	Name string `xml:"groupName"`
}

// SecurityGroupNames is a convenience function that
// returns a slice of security groups with the given names.
func SecurityGroupNames(names ...string) []SecurityGroup {
	g := make([]SecurityGroup, len(names))
	for i, name := range names {
		g[i] = SecurityGroup{Name: name}
	}
	return g
}

// SecurityGroupNames is a convenience function that
// returns a slice of security groups with the given ids.
func SecurityGroupIds(ids ...string) []SecurityGroup {
	g := make([]SecurityGroup, len(ids))
	for i, id := range ids {
		g[i] = SecurityGroup{Id: id}
	}
	return g
}

// SecurityGroups returns details about security groups in EC2.  Both parameters
// are optional, and if provided will limit the security groups returned to those
// matching the given groups or filtering rules.
//
// See http://goo.gl/k12Uy for more details.
func (ec2 *EC2) SecurityGroups(groups []SecurityGroup, filter *Filter) (resp *SecurityGroupsResp, err error) {
	params := makeParams("DescribeSecurityGroups")
	i, j := 1, 1
	for _, g := range groups {
		if g.Id != "" {
			params["GroupId."+strconv.Itoa(i)] = g.Id
			i++
		} else {
			params["GroupName."+strconv.Itoa(j)] = g.Name
			j++
		}
	}
	filter.addParams(params)

	resp = &SecurityGroupsResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteSecurityGroup removes the given security group in EC2.
//
// See http://goo.gl/QJJDO for more details.
func (ec2 *EC2) DeleteSecurityGroup(group SecurityGroup) (resp *SimpleResp, err error) {
	params := makeParams("DeleteSecurityGroup")
	if group.Id != "" {
		params["GroupId"] = group.Id
	} else {
		params["GroupName"] = group.Name
	}

	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// AuthorizeSecurityGroup creates an allowance for clients matching the provided
// rules to access instances within the given security group.
//
// See http://goo.gl/u2sDJ for more details.
func (ec2 *EC2) AuthorizeSecurityGroup(group SecurityGroup, perms []IPPerm) (resp *SimpleResp, err error) {
	return ec2.authOrRevoke("AuthorizeSecurityGroupIngress", group, perms)
}

// RevokeSecurityGroup revokes permissions from a group.
//
// See http://goo.gl/ZgdxA for more details.
func (ec2 *EC2) RevokeSecurityGroup(group SecurityGroup, perms []IPPerm) (resp *SimpleResp, err error) {
	return ec2.authOrRevoke("RevokeSecurityGroupIngress", group, perms)
}

func (ec2 *EC2) authOrRevoke(op string, group SecurityGroup, perms []IPPerm) (resp *SimpleResp, err error) {
	params := makeParams(op)
	if group.Id != "" {
		params["GroupId"] = group.Id
	} else {
		params["GroupName"] = group.Name
	}

	for i, perm := range perms {
		prefix := "IpPermissions." + strconv.Itoa(i+1)
		params[prefix+".IpProtocol"] = perm.Protocol
		params[prefix+".FromPort"] = strconv.Itoa(perm.FromPort)
		params[prefix+".ToPort"] = strconv.Itoa(perm.ToPort)
		for j, ip := range perm.SourceIPs {
			params[prefix+".IpRanges."+strconv.Itoa(j+1)+".CidrIp"] = ip
		}
		for j, g := range perm.SourceGroups {
			subprefix := prefix + ".Groups." + strconv.Itoa(j+1)
			if g.OwnerId != "" {
				params[subprefix+".UserId"] = g.OwnerId
			}
			if g.Id != "" {
				params[subprefix+".GroupId"] = g.Id
			} else {
				params[subprefix+".GroupName"] = g.Name
			}
		}
	}

	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil

}

// Tag represents key-value metadata used to classify and organize
// EC2 instances.
//
// See http://goo.gl/bncl3 for more details
type Tag struct {
	Key   string `xml:"key"`
	Value string `xml:"value"`
}

// CreateTags adds or overwrites one or more tags for the specified instance ids.
//
// See http://goo.gl/Vmkqc for more details
func (ec2 *EC2) CreateTags(instIds []string, tags []Tag) (resp *SimpleResp, err error) {
	params := makeParams("CreateTags")
	addParamsList(params, "ResourceId", instIds)

	for j, tag := range tags {
		params["Tag."+strconv.Itoa(j+1)+".Key"] = tag.Key
		params["Tag."+strconv.Itoa(j+1)+".Value"] = tag.Value
	}

	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Response to a StartInstances request.
//
// See http://goo.gl/awKeF for more details.
type StartInstanceResp struct {
	RequestId    string                `xml:"requestId"`
	StateChanges []InstanceStateChange `xml:"instancesSet>item"`
}

// Response to a StopInstances request.
//
// See http://goo.gl/436dJ for more details.
type StopInstanceResp struct {
	RequestId    string                `xml:"requestId"`
	StateChanges []InstanceStateChange `xml:"instancesSet>item"`
}

// StartInstances starts an Amazon EBS-backed AMI that you've previously stopped.
//
// See http://goo.gl/awKeF for more details.
func (ec2 *EC2) StartInstances(ids ...string) (resp *StartInstanceResp, err error) {
	params := makeParams("StartInstances")
	addParamsList(params, "InstanceId", ids)
	resp = &StartInstanceResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// StopInstances requests stopping one or more Amazon EBS-backed instances.
//
// See http://goo.gl/436dJ for more details.
func (ec2 *EC2) StopInstances(ids ...string) (resp *StopInstanceResp, err error) {
	params := makeParams("StopInstances")
	addParamsList(params, "InstanceId", ids)
	resp = &StopInstanceResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// RebootInstance requests a reboot of one or more instances. This operation is asynchronous;
// it only queues a request to reboot the specified instance(s). The operation will succeed
// if the instances are valid and belong to you.
//
// Requests to reboot terminated instances are ignored.
//
// See http://goo.gl/baoUf for more details.
func (ec2 *EC2) RebootInstances(ids ...string) (resp *SimpleResp, err error) {
	params := makeParams("RebootInstances")
	addParamsList(params, "InstanceId", ids)
	resp = &SimpleResp{}
	err = ec2.query(params, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

type CreateInternetGatewayResp struct {
	InternetGateway InternetGateway `xml:"internetGateway"`
	RequestId       string          `xml:"requestId"`
}

type InternetGateway struct {
	InternetGatewayId string `xml:"internetGatewayId"`
}

func (ec2 *EC2) CreateInternetGateway() (*CreateInternetGatewayResp, error) {
	resp := &CreateInternetGatewayResp{}
	err := ec2.query(makeParams("CreateInternetGateway"), resp)
	return resp, err
}

func (ec2 *EC2) AttachInternetGateway(gatewayId, vpcId string) (*SimpleResp, error) {
	params := makeParams("AttachInternetGateway")
	params["InternetGatewayId"] = gatewayId
	params["VpcId"] = vpcId

	resp := &SimpleResp{}
	err := ec2.query(params, resp)
	return resp, err
}

type Route struct {
	RouteTableId         string
	DestinationCIDRBlock string
	GatewayId            string
	InstanceId           string
	NetworkInterfaceId   string
}

func (ec2 *EC2) CreateRoute(r *Route) (*SimpleResp, error) {
	params := makeParams("CreateRoute")
	params["RouteTableId"] = r.RouteTableId
	params["DestinationCidrBlock"] = r.DestinationCIDRBlock
	if r.GatewayId != "" {
		params["GatewayId"] = r.GatewayId
	}
	if r.InstanceId != "" {
		params["InstanceId"] = r.InstanceId
	}
	if r.NetworkInterfaceId != "" {
		params["NetworkInterfaceId"] = r.NetworkInterfaceId
	}

	resp := &SimpleResp{}
	err := ec2.query(params, resp)
	return resp, err
}

type RouteTable struct {
	RouteTableId string         `xml:"routeTableId"`
	VpcId        string         `xml:"vpcId"`
	RouteSet     []RouteSetItem `xml:"routeSet>item"`
}

type RouteSetItem struct {
	DestinationCIDRBlock string `xml:"destinationCidrBlock"`
	GatewayId            string `xml:"gatewayId"`
	State                string `xml:"state"`
}

type CreateRouteTableResp struct {
	RouteTable RouteTable `xml:"routeTable"`
	RequestId  string     `xml:"requestId"`
}

func (ec2 *EC2) CreateRouteTable(vpcId string) (*CreateRouteTableResp, error) {
	params := makeParams("CreateRouteTable")
	params["VpcId"] = vpcId

	resp := &CreateRouteTableResp{}
	err := ec2.query(params, resp)
	return resp, err
}

type RouteTablesResp struct {
	RequestId   string       `xml:"requestId"`
	RouteTables []RouteTable `xml:"routeTableSet>item"`
}

func (ec2 *EC2) RouteTables(routeTableIds []string, filter *Filter) (*RouteTablesResp, error) {
	params := makeParams("DescribeRouteTables")
	addParamsList(params, "RouteTableId", routeTableIds)
	filter.addParams(params)
	resp := &RouteTablesResp{}
	err := ec2.query(params, resp)
	return resp, err
}

func (ec2 *EC2) DeleteInternetGateway(gatewayId string) (*SimpleResp, error) {
	params := makeParams("DeleteInternetGateway")
	params["InternetGatewayId"] = gatewayId
	resp := &SimpleResp{}
	err := ec2.query(params, resp)
	return resp, err
}

func (ec2 *EC2) DetachInternetGateway(gatewayId, vpcId string) (*SimpleResp, error) {
	params := makeParams("DetachInternetGateway")
	params["InternetGatewayId"] = gatewayId
	params["VpcId"] = vpcId
	resp := &SimpleResp{}
	err := ec2.query(params, resp)
	return resp, err
}

type ImportKeyPairResp struct {
	RequestId   string `xml:"requestId"`
	Name        string `xml:"keyName"`
	Fingerprint string `xml:"keyFingerprint"`
}

func (ec2 *EC2) ImportKeyPair(name, key string) (*ImportKeyPairResp, error) {
	params := makeParams("ImportKeyPair")
	params["KeyName"] = name
	params["PublicKeyMaterial"] = key
	resp := &ImportKeyPairResp{}
	err := ec2.query(params, resp)
	return resp, err
}

func (ec2 *EC2) DeleteKeyPair(name string) (*SimpleResp, error) {
	params := makeParams("DeleteKeyPair")
	params["KeyName"] = name
	resp := &SimpleResp{}
	err := ec2.query(params, resp)
	return resp, err
}

type AccountAttributesResp struct {
	RequestID  string             `xml:"requestId"`
	Attributes []AccountAttribute `xml:"accountAttributeSet>item"`
}

type AccountAttribute struct {
	Name   string   `xml:"attributeName"`
	Values []string `xml:"attributeValueSet>item>attributeValue"`
}

func (ec2 *EC2) AccountAttributes() (*AccountAttributesResp, error) {
	params := makeParamsVPC("DescribeAccountAttributes")
	params["AttributeName.1"] = "supported-platforms"
	params["AttributeName.2"] = "default-vpc"
	resp := &AccountAttributesResp{}
	err := ec2.query(params, resp)
	return resp, err
}
