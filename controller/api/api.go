package api

import (
	"bytes"
	context "context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	fmt "fmt"
	"os"
	"path"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	host "github.com/flynn/flynn/host/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/version"
	router "github.com/flynn/flynn/router/types"
	"github.com/golang/protobuf/ptypes"
	durpb "github.com/golang/protobuf/ptypes/duration"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

func NewStatusResponse(healthy bool, detail []byte) *StatusResponse {
	res := &StatusResponse{
		Status:  StatusResponse_HEALTHY,
		Detail:  detail,
		Version: version.String(),
	}
	if !healthy {
		res.Status = StatusResponse_UNHEALTHY
	}
	return res
}

type authKey struct {
	key string
}

func (k *authKey) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"Auth-Key": k.key,
	}, nil
}

func (k *authKey) RequireTransportSecurity() bool {
	return false
}

func WithAuthKey(key string) grpc.DialOption {
	return grpc.WithPerRPCCredentials(&authKey{key})
}

func MatchLabelFilters(labels map[string]string, labelFilters []*LabelFilter) bool {
	if len(labelFilters) == 0 {
		return true
	}
	for _, f := range labelFilters {
		if f.Match(labels) {
			return true
		}
	}
	return false
}

func (f *LabelFilter) Match(labels map[string]string) bool {
	for _, e := range f.Expressions {
		if !e.Match(labels) {
			return false
		}
	}
	return true
}

func (e *LabelFilter_Expression) Match(labels map[string]string) bool {
	switch e.Op {
	case LabelFilter_Expression_OP_IN:
		if v, ok := labels[e.Key]; ok {
			for _, ev := range e.Values {
				if v == ev {
					return true
				}
			}
		}
		return false
	case LabelFilter_Expression_OP_NOT_IN:
		if v, ok := labels[e.Key]; ok {
			for _, ev := range e.Values {
				if v == ev {
					return false
				}
			}
		}
	case LabelFilter_Expression_OP_EXISTS:
		if _, ok := labels[e.Key]; !ok {
			return false
		}
	case LabelFilter_Expression_OP_NOT_EXISTS:
		if _, ok := labels[e.Key]; ok {
			return false
		}
	}
	return true
}

type ReleaseTypeMatcher struct {
	types map[ReleaseType]struct{}
}

func NewReleaseTypeMatcher(types []ReleaseType) *ReleaseTypeMatcher {
	_types := make(map[ReleaseType]struct{}, len(types))
	for _, t := range types {
		_types[t] = struct{}{}
	}
	return &ReleaseTypeMatcher{types: _types}
}

func (m *ReleaseTypeMatcher) Match(t ReleaseType) bool {
	if len(m.types) == 0 {
		return true
	}
	if _, ok := m.types[ReleaseType_ANY]; ok {
		return true
	}
	if _, ok := m.types[t]; ok {
		return true
	}
	return false
}

func ParseIDsFromNameFilters(nameFilters []string, resource string) []string {
	ids := make([]string, 0, len(nameFilters))
	for _, name := range nameFilters {
		appID := ParseIDFromName(name, resource)
		if appID == "" || !strings.HasSuffix(name, appID) {
			continue
		}
		ids = append(ids, appID)
	}
	return ids
}

func ParseIDFromName(name string, resource string) string {
	parts := strings.Split(name, "/")
	idMap := make(map[string]string, len(parts)/2)
	for i := 0; i < len(parts)-1; i += 2 {
		if i == len(parts) {
			return idMap[resource]
		}
		resourceName := parts[i]
		resourceID := parts[i+1]
		idMap[resourceName] = resourceID
	}
	return idMap[resource]
}

func NewControllerLabelFilters(from []*LabelFilter) []ct.LabelFilter {
	to := make([]ct.LabelFilter, 0, len(from))
	for _, f := range from {
		to = append(to, NewControllerLabelFilter(f))
	}
	return to
}

func NewControllerLabelFilter(from *LabelFilter) ct.LabelFilter {
	to := make(ct.LabelFilter, 0, len(from.Expressions))
	for _, e := range from.Expressions {
		to = append(to, NewControllerLabelFilterExpression(e))
	}
	return to
}

func NewControllerLabelFilterExpression(from *LabelFilter_Expression) *ct.LabelFilterExpression {
	return &ct.LabelFilterExpression{
		Op:     ct.LabelFilterExpressionOp(from.Op),
		Key:    from.Key,
		Values: from.Values,
	}
}

func Error(err error) error {
	if s, ok := status.FromError(err); ok {
		return s.Err()
	}
	if e, ok := err.(hh.JSONError); ok {
		return status.New(toStatusCode(e.Code), e.Message).Err()
	}
	code := codes.Unknown
	if err == controller.ErrNotFound {
		code = codes.NotFound
	}
	switch err.(type) {
	case ct.ValidationError, *ct.ValidationError:
		code = codes.InvalidArgument
	}
	return status.Error(code, err.Error())
}

// toStatusCode converts a httphelper.ErrorCode to a gRPC status code
func toStatusCode(code hh.ErrorCode) codes.Code {
	switch code {
	case hh.NotFoundErrorCode, hh.ObjectNotFoundErrorCode:
		return codes.NotFound
	case hh.ObjectExistsErrorCode:
		return codes.AlreadyExists
	case hh.ConflictErrorCode, hh.PreconditionFailedErrorCode:
		return codes.FailedPrecondition
	case hh.SyntaxErrorCode, hh.ValidationErrorCode, hh.RequestBodyTooBigErrorCode:
		return codes.InvalidArgument
	case hh.UnauthorizedErrorCode:
		return codes.PermissionDenied
	case hh.UnknownErrorCode:
		return codes.Unknown
	case hh.RatelimitedErrorCode:
		return codes.ResourceExhausted
	case hh.ServiceUnavailableErrorCode:
		return codes.Unavailable
	default:
		return codes.Unknown
	}
}

func NewTimestamp(t *time.Time) *tspb.Timestamp {
	if t == nil {
		return nil
	}
	tp, _ := ptypes.TimestampProto(*t)
	return tp
}

func NewGoTimestamp(t *tspb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	ts, _ := ptypes.Timestamp(t)
	return &ts
}

func NewDuration(d *time.Duration) *durpb.Duration {
	if d == nil {
		return nil
	}
	return ptypes.DurationProto(*d)
}

func NewGoDuration(d *durpb.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	dur, _ := ptypes.Duration(d)
	return &dur
}

func NewApp(a *ct.App) *App {
	var releaseName string
	if a.ReleaseID != "" {
		releaseName = path.Join("apps", a.ID, "releases", a.ReleaseID)
	}
	return &App{
		Name:          path.Join("apps", a.ID),
		DisplayName:   a.Name,
		Labels:        a.Meta,
		Strategy:      a.Strategy,
		Release:       releaseName,
		DeployTimeout: a.DeployTimeout,
		CreateTime:    NewTimestamp(a.CreatedAt),
		UpdateTime:    NewTimestamp(a.UpdatedAt),
	}
}

func (a *App) ControllerType() *ct.App {
	return &ct.App{
		ID:            ParseIDFromName(a.Name, "apps"),
		Name:          a.DisplayName,
		Meta:          a.Labels,
		Strategy:      a.Strategy,
		ReleaseID:     ParseIDFromName(a.Release, "releases"),
		DeployTimeout: a.DeployTimeout,
		CreatedAt:     NewGoTimestamp(a.CreateTime),
		UpdatedAt:     NewGoTimestamp(a.UpdateTime),
	}
}

func NewPorts(from []ct.Port) []*Port {
	to := make([]*Port, len(from))
	for i, p := range from {
		to[i] = &Port{
			Port:    int32(p.Port),
			Proto:   p.Proto,
			Service: NewService(p.Service),
		}
	}
	return to
}

func NewControllerPorts(from []*Port) []ct.Port {
	to := make([]ct.Port, len(from))
	for i, p := range from {
		to[i] = ct.Port{
			Port:    int(p.Port),
			Proto:   p.Proto,
			Service: NewControllerService(p.Service),
		}
	}
	return to
}

func NewService(from *host.Service) *HostService {
	return &HostService{
		DisplayName: from.Name,
		Create:      from.Create,
		Check:       NewHealthCheck(from.Check),
	}
}

func NewControllerService(from *HostService) *host.Service {
	return &host.Service{
		Name:   from.DisplayName,
		Create: from.Create,
		Check:  NewControllerHealthCheck(from.Check),
	}
}

func NewHealthCheck(from *host.HealthCheck) *HostHealthCheck {
	return &HostHealthCheck{
		Type:         from.Type,
		Interval:     NewDuration(&from.Interval),
		Threshold:    int32(from.Threshold),
		KillDown:     from.KillDown,
		StartTimeout: NewDuration(&from.StartTimeout),
		Path:         from.Path,
		Host:         from.Host,
		Match:        from.Match,
		Status:       int32(from.Status),
	}
}

func NewControllerHealthCheck(from *HostHealthCheck) *host.HealthCheck {
	return &host.HealthCheck{
		Type:         from.Type,
		Interval:     *NewGoDuration(from.Interval),
		Threshold:    int(from.Threshold),
		KillDown:     from.KillDown,
		StartTimeout: *NewGoDuration(from.StartTimeout),
		Path:         from.Path,
		Host:         from.Host,
		Match:        from.Match,
		Status:       int(from.Status),
	}
}

func NewVolumes(from []ct.VolumeReq) []*VolumeReq {
	to := make([]*VolumeReq, 0, len(from))
	for _, r := range from {
		to = append(to, &VolumeReq{
			Path:         r.Path,
			DeleteOnStop: r.DeleteOnStop,
		})
	}
	return to
}

func NewControllerVolumes(from []*VolumeReq) []ct.VolumeReq {
	to := make([]ct.VolumeReq, 0, len(from))
	for _, r := range from {
		to = append(to, ct.VolumeReq{
			Path:         r.Path,
			DeleteOnStop: r.DeleteOnStop,
		})
	}
	return to
}

func NewResources(from resource.Resources) map[string]*HostResourceSpec {
	to := make(map[string]*HostResourceSpec, len(from))
	for k, v := range from {
		spec := &HostResourceSpec{}
		if v.Limit != nil {
			spec.Limit = *v.Limit
		}
		if v.Request != nil {
			spec.Request = *v.Request
		}
		to[string(k)] = spec
	}
	return to
}

func NewControllerResources(from map[string]*HostResourceSpec) resource.Resources {
	to := make(resource.Resources, len(from))
	for k, v := range from {
		spec := resource.Spec{}
		// TODO(jvatic): is zero a valid spec limit? (should there be a seperate
		// flag for nil?)
		if v.Limit > 0 {
			spec.Limit = &v.Limit
		}
		// TODO(jvatic): is zero a valid spec request? (should there be a seperate
		// flag for nil?)
		if v.Request > 0 {
			spec.Request = &v.Request
		}
		to[resource.Type(k)] = spec
	}
	return resource.Resources{}
}

func NewMounts(from []host.Mount) []*HostMount {
	to := make([]*HostMount, 0, len(from))
	for _, v := range from {
		to = append(to, &HostMount{
			Location:  v.Location,
			Target:    v.Target,
			Writeable: v.Writeable,
			Device:    v.Device,
			Data:      v.Data,
			Flags:     int32(v.Flags),
		})
	}
	return to
}

func NewControllerMounts(from []*HostMount) []host.Mount {
	to := make([]host.Mount, 0, len(from))
	for _, v := range from {
		to = append(to, host.Mount{
			Location:  v.Location,
			Target:    v.Target,
			Writeable: v.Writeable,
			Device:    v.Device,
			Data:      v.Data,
			Flags:     int(v.Flags),
		})
	}
	return to
}

func NewAllowedDevices(from []*host.Device) []*LibContainerDevice {
	to := make([]*LibContainerDevice, 0, len(from))
	for _, v := range from {
		to = append(to, &LibContainerDevice{
			Type:        int32(v.Type),
			Path:        v.Path,
			Major:       v.Major,
			Minor:       v.Minor,
			Permissions: v.Permissions,
			FileMode:    uint32(v.FileMode),
			Uid:         v.Uid,
			Gid:         v.Gid,
			Allow:       v.Allow,
		})
	}
	return to
}

func NewControllerAllowedDevices(from []*LibContainerDevice) []*host.Device {
	to := make([]*host.Device, 0, len(from))
	for _, v := range from {
		to = append(to, &host.Device{
			Type:        rune(v.Type),
			Path:        v.Path,
			Major:       v.Major,
			Minor:       v.Minor,
			Permissions: v.Permissions,
			FileMode:    os.FileMode(v.FileMode),
			Uid:         v.Uid,
			Gid:         v.Gid,
			Allow:       v.Allow,
		})
	}
	return to
}

func NewProcesses(from map[string]ct.ProcessType) map[string]*ProcessType {
	if len(from) == 0 {
		return nil
	}
	to := make(map[string]*ProcessType, len(from))
	for k, t := range from {
		to[k] = &ProcessType{
			Args:              t.Args,
			Env:               t.Env,
			Ports:             NewPorts(t.Ports),
			Volumes:           NewVolumes(t.Volumes),
			Omni:              t.Omni,
			HostNetwork:       t.HostNetwork,
			HostPidNamespace:  t.HostPIDNamespace,
			Service:           t.Service,
			Resurrect:         t.Resurrect,
			Resources:         NewResources(t.Resources),
			Mounts:            NewMounts(t.Mounts),
			LinuxCapabilities: t.LinuxCapabilities,
			AllowedDevices:    NewAllowedDevices(t.AllowedDevices),
			WriteableCgroups:  t.WriteableCgroups,
		}
	}
	return to
}

func NewControllerProcesses(from map[string]*ProcessType) map[string]ct.ProcessType {
	to := make(map[string]ct.ProcessType, len(from))
	for k, t := range from {
		to[k] = ct.ProcessType{
			Args:              t.Args,
			Env:               t.Env,
			Ports:             NewControllerPorts(t.Ports),
			Volumes:           NewControllerVolumes(t.Volumes),
			Omni:              t.Omni,
			HostNetwork:       t.HostNetwork,
			HostPIDNamespace:  t.HostPidNamespace,
			Service:           t.Service,
			Resurrect:         t.Resurrect,
			Resources:         NewControllerResources(t.Resources),
			Mounts:            NewControllerMounts(t.Mounts),
			LinuxCapabilities: t.LinuxCapabilities,
			AllowedDevices:    NewControllerAllowedDevices(t.AllowedDevices),
			WriteableCgroups:  t.WriteableCgroups,
		}
	}
	return to
}

func NewRelease(r *ct.Release) *Release {
	return &Release{
		Name:       fmt.Sprintf("apps/%s/releases/%s", r.AppID, r.ID),
		Artifacts:  r.ArtifactIDs,
		Env:        r.Env,
		Labels:     r.Meta,
		Processes:  NewProcesses(r.Processes),
		CreateTime: NewTimestamp(r.CreatedAt),
	}
}

func (r *Release) ControllerType() *ct.Release {
	return &ct.Release{
		AppID:       ParseIDFromName(r.Name, "apps"),
		ID:          ParseIDFromName(r.Name, "releases"),
		ArtifactIDs: r.Artifacts,
		Env:         r.Env,
		Meta:        r.Labels,
		Processes:   NewControllerProcesses(r.Processes),
		CreatedAt:   NewGoTimestamp(r.CreateTime),
	}
}

func (s ScaleRequestState) ControllerType() ct.ScaleRequestState {
	switch s {
	case ScaleRequestState_SCALE_CANCELLED:
		return ct.ScaleRequestStateCancelled
	case ScaleRequestState_SCALE_COMPLETE:
		return ct.ScaleRequestStateComplete
	default:
		return ct.ScaleRequestStatePending
	}
}

func NewScaleRequest(ctScaleReq *ct.ScaleRequest) *ScaleRequest {
	state := ScaleRequestState_SCALE_PENDING
	switch ctScaleReq.State {
	case ct.ScaleRequestStateCancelled:
		state = ScaleRequestState_SCALE_CANCELLED
	case ct.ScaleRequestStateComplete:
		state = ScaleRequestState_SCALE_COMPLETE
	}

	var newProcesses map[string]int32
	if ctScaleReq.NewProcesses != nil {
		newProcesses = NewDeploymentProcesses(*ctScaleReq.NewProcesses)
	}

	var newTags map[string]*DeploymentProcessTags
	if ctScaleReq.NewTags != nil {
		newTags = NewDeploymentTags(*ctScaleReq.NewTags)
	}

	return &ScaleRequest{
		Parent:       fmt.Sprintf("apps/%s/releases/%s", ctScaleReq.AppID, ctScaleReq.ReleaseID),
		Name:         fmt.Sprintf("apps/%s/releases/%s/scales/%s", ctScaleReq.AppID, ctScaleReq.ReleaseID, ctScaleReq.ID),
		State:        state,
		OldProcesses: NewDeploymentProcesses(ctScaleReq.OldProcesses),
		NewProcesses: newProcesses,
		OldTags:      NewDeploymentTags(ctScaleReq.OldTags),
		NewTags:      newTags,
		CreateTime:   NewTimestamp(ctScaleReq.CreatedAt),
		UpdateTime:   NewTimestamp(ctScaleReq.UpdatedAt),
	}
}

func (req *ScaleRequest) ControllerType() *ct.ScaleRequest {
	var newProcesses map[string]int
	if req.NewProcesses != nil {
		newProcesses = NewControllerDeploymentProcesses(req.NewProcesses)
	}

	var newTags map[string]map[string]string
	if req.NewTags != nil {
		newTags = NewControllerDeploymentTags(req.NewTags)
	}

	return &ct.ScaleRequest{
		ID:           ParseIDFromName(req.Name, "scales"),
		AppID:        ParseIDFromName(req.Parent, "apps"),
		ReleaseID:    ParseIDFromName(req.Parent, "releases"),
		State:        req.State.ControllerType(),
		OldProcesses: NewControllerDeploymentProcesses(req.OldProcesses),
		NewProcesses: &newProcesses,
		OldTags:      NewControllerDeploymentTags(req.OldTags),
		NewTags:      &newTags,
		CreatedAt:    NewGoTimestamp(req.CreateTime),
		UpdatedAt:    NewGoTimestamp(req.UpdateTime),
	}
}

func (csr *CreateScaleRequest) ControllerType() *ct.ScaleRequest {
	return (&ScaleRequest{
		Parent:       csr.Parent,
		NewProcesses: csr.Processes,
		NewTags:      csr.Tags,
	}).ControllerType()
}

func NewDeploymentProcesses(from map[string]int) map[string]int32 {
	if from == nil {
		return nil
	}
	to := make(map[string]int32, len(from))
	for k, v := range from {
		to[k] = int32(v)
	}
	return to
}

func NewControllerDeploymentProcesses(from map[string]int32) map[string]int {
	if from == nil {
		return nil
	}
	to := make(map[string]int, len(from))
	for k, v := range from {
		to[k] = int(v)
	}
	return to
}

func NewDeploymentTags(from map[string]map[string]string) map[string]*DeploymentProcessTags {
	if from == nil {
		return nil
	}
	to := make(map[string]*DeploymentProcessTags, len(from))
	for k, v := range from {
		to[k] = &DeploymentProcessTags{Tags: v}
	}
	return to
}

func NewControllerDeploymentTags(from map[string]*DeploymentProcessTags) map[string]map[string]string {
	if from == nil {
		return nil
	}
	to := make(map[string]map[string]string, len(from))
	for k, v := range from {
		to[k] = v.Tags
	}
	return to
}

func NewDeploymentStatus(from string) DeploymentStatus {
	switch from {
	case "pending":
		return DeploymentStatus_PENDING
	case "failed":
		return DeploymentStatus_FAILED
	case "running":
		return DeploymentStatus_RUNNING
	case "complete":
		return DeploymentStatus_COMPLETE
	}
	return DeploymentStatus_PENDING
}

func (s DeploymentStatus) ControllerType() string {
	switch s {
	case DeploymentStatus_FAILED:
		return "failed"
	case DeploymentStatus_RUNNING:
		return "running"
	case DeploymentStatus_COMPLETE:
		return "complete"
	default:
		return "pending"
	}
}

func NewExpandedDeployment(from *ct.ExpandedDeployment) *ExpandedDeployment {
	convertReleaseType := func(releaseType ct.ReleaseType) ReleaseType {
		switch releaseType {
		case ct.ReleaseTypeConfig:
			return ReleaseType_CONFIG
		case ct.ReleaseTypeCode:
			return ReleaseType_CODE
		default:
			return ReleaseType_ANY
		}
	}

	var oldRelease *Release
	if from.OldRelease != nil {
		oldRelease = NewRelease(from.OldRelease)
	}
	newRelease := NewRelease(from.NewRelease)
	return &ExpandedDeployment{
		Name:          fmt.Sprintf("apps/%s/deployments/%s", from.AppID, from.ID),
		OldRelease:    oldRelease,
		NewRelease:    newRelease,
		Type:          convertReleaseType(from.Type),
		Strategy:      from.Strategy,
		Status:        NewDeploymentStatus(from.Status),
		Processes:     NewDeploymentProcesses(from.Processes),
		Tags:          NewDeploymentTags(from.Tags),
		DeployTimeout: from.DeployTimeout,
		CreateTime:    NewTimestamp(from.CreatedAt),
		EndTime:       NewTimestamp(from.FinishedAt),
	}
}

func NewJobState(from ct.JobState) DeploymentEvent_JobState {
	switch from {
	case "pending":
		return DeploymentEvent_PENDING
	case "blocked":
		return DeploymentEvent_BLOCKED
	case "starting":
		return DeploymentEvent_STARTING
	case "up":
		return DeploymentEvent_UP
	case "stopping":
		return DeploymentEvent_STOPPING
	case "down":
		return DeploymentEvent_DOWN
	case "crashed":
		return DeploymentEvent_CRASHED
	case "failed":
		return DeploymentEvent_FAILED
	}
	return DeploymentEvent_PENDING
}

func NewKey(from *router.Key) *Key {
	key := &Key{
		Name:       path.Join("tls-keys", from.ID.String()),
		CreateTime: NewTimestamp(&from.CreatedAt),
	}
	switch from.Algorithm {
	case router.KeyAlgo_ECC_P256:
		key.Algorithm = Key_KEY_ALG_ECC_P256
	case router.KeyAlgo_RSA_2048:
		key.Algorithm = Key_KEY_ALG_RSA_2048
	case router.KeyAlgo_RSA_4096:
		key.Algorithm = Key_KEY_ALG_RSA_4096
	}
	if len(from.Certificates) > 0 {
		key.Certificates = make([]string, len(from.Certificates))
		for i, certID := range from.Certificates {
			key.Certificates[i] = path.Join("certificates", certID.String())
		}
	}
	return key
}

var (
	tlsFeatureExtensionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 24}
	ocspMustStapleValue    = []byte{0x30, 0x03, 0x02, 0x01, 0x05}
)

func NewCertificate(from *router.Certificate) (cert *Certificate) {
	cert = &Certificate{
		Chain:      from.Chain,
		NoStrict:   from.NoStrict,
		Status:     Certificate_STATUS_VALID,
		CreateTime: NewTimestamp(&from.CreatedAt),
	}

	// load the certificate chain
	chain, err := x509.ParseCertificates(bytes.Join(cert.Chain, []byte{}))
	if err != nil {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = err.Error()
		return
	}

	// load the leaf certificate
	if len(chain) == 0 {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = "missing leaf certificate"
		return
	}
	leafCert := chain[0]

	// set Issuer and validity times
	cert.Issuer = leafCert.Issuer.String()
	cert.NotBefore = NewTimestamp(&leafCert.NotBefore)
	cert.NotAfter = NewTimestamp(&leafCert.NotAfter)

	// check if OCSP Must-Staple is set
	for _, ext := range leafCert.Extensions {
		if ext.Id.Equal(tlsFeatureExtensionOID) && bytes.Equal(ext.Value, ocspMustStapleValue) {
			cert.OcspMustStaple = true
			break
		}
	}

	// generate fingerprints
	fingerprint := func(data []byte) []byte {
		s := sha256.Sum256(data)
		return s[:]
	}
	cert.LeafFingerprint = fingerprint(leafCert.RawTBSCertificate)
	cert.SpkiFingerprint = fingerprint(leafCert.RawSubjectPublicKeyInfo)
	cert.ChainFingerprint = fingerprint(bytes.Join(cert.Chain, []byte{}))

	// determine the key algorithm
	keyAlgo, err := router.KeyAlgorithm(leafCert.PublicKey)
	if err != nil {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = err.Error()
		return
	}
	switch keyAlgo {
	case router.KeyAlgo_ECC_P256:
		cert.KeyAlgorithm = Key_KEY_ALG_ECC_P256
	case router.KeyAlgo_RSA_2048:
		cert.KeyAlgorithm = Key_KEY_ALG_RSA_2048
	case router.KeyAlgo_RSA_4096:
		cert.KeyAlgorithm = Key_KEY_ALG_RSA_4096
	}

	// set the Domains from SAN values
	cert.Domains = leafCert.DNSNames
	if len(cert.Domains) == 0 {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = "missing Subject Alternative Name"
		return
	}

	// check the leaf cert has the serverAuth EKU
	hasServerAuth := false
	for _, eku := range leafCert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
			break
		}
	}
	if !hasServerAuth {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = "leaf certificate must have the serverAuth EKU"
		return
	}

	// validate the chain by checking:
	//
	// - the issuer always matches the subject of the next certificate in the chain exactly
	// - the CA attribute is set on all except the leaf
	// - each certificate in the chain either has no EKU, serverAuth, or anyExtendedKeyUsage
	// - all signatures in the chain use a SHA-2 hash algorithm
	supportedSigAlgo := func(cert *x509.Certificate) bool {
		switch cert.SignatureAlgorithm {
		case x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA,
			x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512,
			x509.SHA256WithRSAPSS, x509.SHA384WithRSAPSS, x509.SHA512WithRSAPSS,
			x509.DSAWithSHA256:
			return true
		default:
			return false
		}
	}
	if !supportedSigAlgo(leafCert) {
		cert.Status = Certificate_STATUS_INVALID
		cert.StatusDetail = fmt.Sprintf("leaf certificate uses unsupported signature algorithm %s", leafCert.SignatureAlgorithm)
		return
	}
	if len(chain) > 1 {
		for i := 0; i < len(chain)-1; i++ {
			child := chain[i]
			parent := chain[i+1]
			if !bytes.Equal(child.RawIssuer, parent.RawSubject) {
				cert.Status = Certificate_STATUS_INVALID
				cert.StatusDetail = fmt.Sprintf(
					"the issuer of chain certificate %d (%q) does not match the subject of chain certificate %d (%q)",
					i, child.Issuer, i+1, parent.Subject,
				)
				return
			}
			if !parent.IsCA {
				cert.Status = Certificate_STATUS_INVALID
				cert.StatusDetail = fmt.Sprintf(
					"chain certificate %d (%q) does not have the CA attribute set",
					i+1, parent.Subject,
				)
				return
			}
			if len(parent.ExtKeyUsage) > 0 {
				hasServerAuth := false
				for _, eku := range parent.ExtKeyUsage {
					if eku == x509.ExtKeyUsageServerAuth || eku == x509.ExtKeyUsageAny {
						hasServerAuth = true
						break
					}
				}
				if !hasServerAuth {
					cert.Status = Certificate_STATUS_INVALID
					cert.StatusDetail = fmt.Sprintf("chain certificate %d (%q) must have either the serverAuth or anyExtendedKeyUsage EKU", i+1, parent.Subject)
					return
				}
			}
			if !supportedSigAlgo(parent) {
				cert.Status = Certificate_STATUS_INVALID
				cert.StatusDetail = fmt.Sprintf("chain certificate %d (%q) uses unsupported signature algorithm %s", i+1, parent.Subject, parent.SignatureAlgorithm)
				return
			}
		}
	}

	// check for expired or not yet valid status
	if leafCert.NotAfter.Before(time.Now()) {
		cert.Status = Certificate_STATUS_EXPIRED
		cert.StatusDetail = fmt.Sprintf("certificate expired on %s", leafCert.NotAfter)
	} else if leafCert.NotBefore.After(time.Now()) {
		cert.Status = Certificate_STATUS_FUTURE_NOT_BEFORE
		cert.StatusDetail = fmt.Sprintf("certificate is not valid until %s", leafCert.NotBefore)
	}

	return
}
