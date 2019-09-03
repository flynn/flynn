//go:generate protoc -I/usr/local/include -I./ -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=plugins=grpc:./ ./controller.proto
package protobuf

import (
	context "context"
	fmt "fmt"
	"os"
	"path"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	host "github.com/flynn/flynn/host/types"
	"github.com/golang/protobuf/ptypes"
	durpb "github.com/golang/protobuf/ptypes/duration"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
)

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

func NewError(err error, message string, args ...interface{}) error {
	errCode := codes.Unknown
	if err == controller.ErrNotFound {
		errCode = codes.NotFound
	}
	switch err.(type) {
	case ct.ValidationError, *ct.ValidationError:
		errCode = codes.InvalidArgument
	}
	return grpc.Errorf(errCode, fmt.Sprintf(message, args...))
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
