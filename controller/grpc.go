package main

import (
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/api"
	"github.com/flynn/flynn/controller/data"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/golang/protobuf/ptypes/empty"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	log "github.com/inconshreveable/log15"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

type grpcAPI struct {
	*controllerAPI

	db *postgres.DB
}

func (g *grpcAPI) authorize(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, grpc.Errorf(codes.Unauthenticated, "metadata missing")
	}

	auth := md["authorization"]
	if len(auth) == 0 || auth[0] == "" {
		return ctx, grpc.Errorf(codes.Unauthenticated, "no Authorization provided")
	}

	token, err := g.authorizer.AuthorizeToken(auth[0])
	if err != nil {
		return ctx, grpc.Errorf(codes.Unauthenticated, err.Error())
	}
	ctx = ctxhelper.NewContextLogger(ctx, g.logger(ctx).New("auth_token_id", token.ID, "auth_user", token.User))

	return ctx, nil
}

func (g *grpcAPI) logger(ctx context.Context) log.Logger {
	log, ok := ctxhelper.LoggerFromContext(ctx)
	if !ok {
		log = logger
	}
	return log
}

func (g *grpcAPI) logRequest(ctx context.Context, rpcMethod string) (context.Context, func(context.Context, error)) {
	startTime := time.Now()

	var clientIP string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		var reqID []string
		if id, ok := ctxhelper.RequestIDFromContext(ctx); ok {
			reqID = []string{id}
		} else {
			reqID = md.Get("x-request-id")
		}
		if len(reqID) == 0 {
			reqID = []string{random.UUID()}
		}
		ctx = ctxhelper.NewContextRequestID(ctx, reqID[0])
		ctx = ctxhelper.NewContextLogger(ctx, g.logger(ctx).New("req_id", reqID[0]))

		xForwardedFor := md.Get("x-forwarded-for")
		if len(xForwardedFor) > 0 {
			clientIPs := strings.Split(xForwardedFor[0], ",")
			if len(clientIPs) > 0 {
				clientIP = strings.TrimSpace(clientIPs[len(clientIPs)-1])
			}
		}
	}
	if clientIP == "" {
		if remoteHost, ok := ctx.Value(ctxKeyRemoteHost).(string); ok {
			clientIP = remoteHost
		}
	}

	g.logger(ctx).Info("gRPC request started", "rpcMethod", rpcMethod, "client_ip", clientIP)
	return ctx, func(ctx context.Context, err error) {
		args := []interface{}{
			"rpcMethod", rpcMethod,
			"duration", time.Since(startTime),
		}
		if err != nil {
			args = append(args, "err", err)
		}
		g.logger(ctx).Info("gRPC request ended", args...)
	}
}

func (g *grpcAPI) subscribeEvents(opts *data.EventSubscriptionOpts) (*data.EventSubscriber, error) {
	eventListener, err := g.maybeStartEventListener()
	if err != nil {
		return nil, api.NewError(err, err.Error())
	}
	sub, err := eventListener.SubscribeWithOpts(opts)
	if err != nil {
		return nil, api.NewError(err, err.Error())
	}
	return sub, nil
}

func (g *grpcAPI) grpcServer() *grpc.Server {
	s := grpc.NewServer(
		grpc.StatsHandler(&grpcStatsHandler{}),
		grpc.StreamInterceptor(g.streamInterceptor),
		grpc.UnaryInterceptor(g.unaryInterceptor),
	)
	api.RegisterControllerServer(s, g)
	// Register reflection service on gRPC server.
	reflection.Register(s)
	return s
}

type grpcStatsHandler struct{}

func (g *grpcStatsHandler) TagRPC(ctx context.Context, rpcTagInfo *stats.RPCTagInfo) context.Context {
	return ctx
}

func (g *grpcStatsHandler) HandleRPC(context.Context, stats.RPCStats) {
}

const ctxKeyRemoteHost = "remote-host"

func (g *grpcStatsHandler) TagConn(ctx context.Context, connTagInfo *stats.ConnTagInfo) context.Context {
	remoteHost, _, _ := net.SplitHostPort(connTagInfo.RemoteAddr.String())
	ctx = context.WithValue(ctx, ctxKeyRemoteHost, remoteHost)
	return ctx
}

func (g *grpcStatsHandler) HandleConn(context.Context, stats.ConnStats) {
}

func (g *grpcAPI) streamInterceptor(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	ctx, logRequestEnd := g.logRequest(stream.Context(), info.FullMethod)
	defer func() {
		logRequestEnd(ctx, err)
	}()

	ctx, err = g.authorize(stream.Context())
	if err != nil {
		return err
	}

	wrappedStream := middleware.WrapServerStream(stream)
	wrappedStream.WrappedContext = ctx
	return handler(srv, wrappedStream)
}

func (g *grpcAPI) unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (res interface{}, err error) {
	ctx, logRequestEnd := g.logRequest(ctx, info.FullMethod)
	defer func() {
		logRequestEnd(ctx, err)
	}()

	ctx, err = g.authorize(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

func (g *grpcAPI) Status(context.Context, *empty.Empty) (*api.StatusResponse, error) {
	healthy := true
	if err := g.db.Exec("ping"); err != nil {
		healthy = false
	}
	return api.NewStatusResponse(healthy, nil), nil
}

func (g *grpcAPI) listApps(req *api.StreamAppsRequest) ([]*api.App, *data.PageToken, error) {
	pageSize := int(req.GetPageSize())
	pageToken, err := data.ParsePageToken(req.PageToken)
	if err != nil {
		return nil, nil, err
	}

	if pageSize > 0 {
		pageToken.Size = pageSize
	} else {
		pageSize = pageToken.Size
	}

	appIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
	ctApps, nextPageToken, err := g.appRepo.ListPage(data.ListAppOptions{
		PageToken:    *pageToken,
		AppIDs:       appIDs,
		LabelFilters: api.NewControllerLabelFilters(req.GetLabelFilters()),
	})
	if err != nil {
		return nil, nil, err
	}

	if pageSize == 0 {
		pageSize = len(ctApps)
	}

	if len(appIDs) == 0 {
		appIDs = nil
	}

	apps := make([]*api.App, 0, pageSize)
	n := 0
	for _, a := range ctApps {
		apps = append(apps, api.NewApp(a))
		n++

		if n == pageSize {
			break
		}
	}

	return apps, nextPageToken, nil
}

func (g *grpcAPI) StreamApps(req *api.StreamAppsRequest, stream api.Controller_StreamAppsServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	var sub *data.EventSubscriber
	if !unary {
		appIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
		var err error
		sub, err = g.subscribeEvents(&data.EventSubscriptionOpts{
			AppIDs:      appIDs,
			ObjectTypes: []ct.EventType{ct.EventTypeApp, ct.EventTypeAppDeletion, ct.EventTypeAppRelease},
		})
		if err != nil {
			return api.NewError(err, err.Error())
		}
		defer sub.Close()
	}

	apps, nextPageToken, err := g.listApps(req)
	if err != nil {
		return api.NewError(err, err.Error())
	}

	stream.Send(&api.StreamAppsResponse{
		Apps:          apps,
		NextPageToken: nextPageToken.String(),
		PageComplete:  true,
	})

	if sub == nil {
		return nil
	}

	maybeSendApp := func(event *ct.ExpandedEvent, app *api.App) {
		shouldSend := false
		if (req.StreamCreates && event.Op == ct.EventOpCreate) || (req.StreamUpdates && event.Op == ct.EventOpUpdate) || (req.StreamUpdates && event.ObjectType == ct.EventTypeAppRelease) {
			shouldSend = api.MatchLabelFilters(app.Labels, req.GetLabelFilters())
		}
		if shouldSend {
			stream.Send(&api.StreamAppsResponse{
				Apps: []*api.App{app},
			})
		}
	}

outer:
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				err = sub.Err
				break outer
			}
			switch event.ObjectType {
			case ct.EventTypeApp:
				var ctApp *ct.App
				if err := json.Unmarshal(event.Data, &ctApp); err != nil {
					logger.Error("error unmarshalling event", "rpcMethod", "StreamApps", "event_id", event.ID, "error", err)
					continue
				}
				maybeSendApp(event, api.NewApp(ctApp))
			case ct.EventTypeAppDeletion:
				if !req.StreamUpdates {
					continue
				}
				var ctAppDeletionEvent *ct.AppDeletionEvent
				if err := json.Unmarshal(event.Data, &ctAppDeletionEvent); err != nil {
					logger.Error("error unmarshalling app deletion event", "rpcMethod", "StreamApps", "event_id", event.ID, "error", err)
					continue
				}
				if ctAppDeletionEvent.AppDeletion == nil {
					continue
				}
				app := api.NewApp(&ct.App{ID: ctAppDeletionEvent.AppDeletion.AppID})
				app.DeleteTime = api.NewTimestamp(event.CreatedAt)
				stream.Send(&api.StreamAppsResponse{
					Apps: []*api.App{app},
				})
			case ct.EventTypeAppRelease:
				if !req.StreamUpdates {
					continue
				}
				ctApp, err := g.appRepo.Get(event.AppID)
				if err != nil {
					logger.Error("error fetching app", "rpcMethod", "StreamApps", "app_id", event.AppID, "error", err)
					continue
				}
				maybeSendApp(event, api.NewApp(ctApp.(*ct.App)))
			}
		case <-stream.Context().Done():
			err = stream.Context().Err()
			break outer
		}
	}
	if err != nil {
		err = api.NewError(err, err.Error())
	}
	return err
}

func (g *grpcAPI) UpdateApp(ctx context.Context, req *api.UpdateAppRequest) (*api.App, error) {
	app := req.App
	data := map[string]interface{}{
		"meta": app.Labels,
	}

	if app.Strategy != "" {
		data["strategy"] = app.Strategy
	}

	if app.DeployTimeout > 0 {
		data["deploy_timeout"] = app.DeployTimeout
	}

	if mask := req.GetUpdateMask(); mask != nil {
		if paths := mask.GetPaths(); len(paths) > 0 {
			maskedData := make(map[string]interface{}, len(paths))
			for _, path := range paths {
				if path == "labels" {
					path = "meta"
				}
				if v, ok := data[path]; ok {
					maskedData[path] = v
				}
			}
			data = maskedData
		}
	}

	ctApp, err := g.appRepo.Update(api.ParseIDFromName(app.Name, "apps"), data)
	if err != nil {
		return nil, api.NewError(err, err.Error())
	}
	return api.NewApp(ctApp.(*ct.App)), nil
}

func (g *grpcAPI) createScale(req *api.CreateScaleRequest) (*api.ScaleRequest, error) {
	appID := api.ParseIDFromName(req.Parent, "apps")
	releaseID := api.ParseIDFromName(req.Parent, "releases")
	processes := parseDeploymentProcesses(req.Config.Processes)
	tags := parseDeploymentTags(req.Config.Tags)

	sub, err := g.subscribeEvents(&data.EventSubscriptionOpts{
		AppIDs:      []string{appID},
		ObjectTypes: []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation},
		ObjectIDs:   nil,
	})
	if err != nil {
		return nil, api.NewError(err, err.Error())
	}
	defer sub.Close()

	scaleReq := &ct.ScaleRequest{
		AppID:     appID,
		ReleaseID: releaseID,
		State:     ct.ScaleRequestStatePending,
	}
	if processes != nil {
		scaleReq.NewProcesses = &processes
	}
	if tags != nil {
		scaleReq.NewTags = &tags
	}
	if _, err := g.formationRepo.AddScaleRequest(scaleReq, false); err != nil {
		return nil, api.NewError(err, err.Error())
	}

	timeout := time.After(ct.DefaultScaleTimeout)
outer:
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				break outer
			}
			switch event.ObjectType {
			case ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation:
				var req ct.ScaleRequest
				if err := json.Unmarshal(event.Data, &req); err != nil {
					continue
				}
				if req.ID != scaleReq.ID {
					continue
				}
				switch req.State {
				case ct.ScaleRequestStateCancelled:
					return nil, status.Error(codes.Canceled, "scale request canceled")
				case ct.ScaleRequestStateComplete:
					break outer
				}
			}
		case <-timeout:
			return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for scale to complete (waited %.f seconds)", ct.DefaultScaleTimeout.Seconds())
		}
	}

	if err := sub.Err; err != nil {
		return nil, api.NewError(err, err.Error())
	}

	return api.NewScaleRequest(scaleReq), nil
}

func (g *grpcAPI) CreateScale(ctx context.Context, req *api.CreateScaleRequest) (*api.ScaleRequest, error) {
	return g.createScale(req)
}

func (g *grpcAPI) StreamScales(req *api.StreamScalesRequest, stream api.Controller_StreamScalesServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	pageSize := int(req.PageSize)
	pageToken, err := data.ParsePageToken(req.PageToken)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid page token: %q", req.PageToken)
	}

	if pageSize > 0 {
		pageToken.Size = pageSize
	} else {
		pageSize = pageToken.Size
	}

	appIDs := api.ParseIDsFromNameFilters(req.NameFilters, "apps")
	releaseIDs := api.ParseIDsFromNameFilters(req.NameFilters, "releases")
	scaleIDs := api.ParseIDsFromNameFilters(req.NameFilters, "scales")

	streamAppIDs := appIDs
	streamScaleIDs := scaleIDs
	if len(releaseIDs) > 0 {
		// we can't filter releaseIDs in the subscription, so don't filter anything
		streamAppIDs = nil
		streamScaleIDs = nil
	}
	sub, err := g.subscribeEvents(&data.EventSubscriptionOpts{
		AppIDs:      streamAppIDs,
		ObjectTypes: []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation},
		ObjectIDs:   streamScaleIDs,
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	defer sub.Close()

	// get all events up until now
	stateFilters := make([]ct.ScaleRequestState, 0, len(req.StateFilters))
	for _, state := range req.StateFilters {
		stateFilters = append(stateFilters, state.ControllerType())
	}
	list, nextPageToken, err := g.formationRepo.ListScaleRequests(data.ListScaleRequestOptions{
		PageToken:    *pageToken,
		AppIDs:       appIDs,
		ReleaseIDs:   releaseIDs,
		ScaleIDs:     scaleIDs,
		StateFilters: stateFilters,
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	scaleRequests := make([]*api.ScaleRequest, 0, len(list))
	for _, ctScale := range list {
		scaleRequests = append(scaleRequests, api.NewScaleRequest(ctScale))
	}
	stream.Send(&api.StreamScalesResponse{
		ScaleRequests: scaleRequests,
		PageComplete:  true,
		NextPageToken: nextPageToken.String(),
	})

	if unary {
		return nil
	}

	stateFilterMap := make(map[api.ScaleRequestState]struct{}, len(req.StateFilters))
	for _, state := range req.StateFilters {
		stateFilterMap[state] = struct{}{}
	}

	releaseIDsMap := make(map[string]struct{}, len(releaseIDs))
	for _, releaseID := range releaseIDs {
		releaseIDsMap[releaseID] = struct{}{}
	}

	appIDsMap := make(map[string]struct{}, len(appIDs))
	for _, appID := range appIDs {
		appIDsMap[appID] = struct{}{}
	}

	scaleIDsMap := make(map[string]struct{}, len(scaleIDs))
	for _, scaleID := range scaleIDs {
		scaleIDsMap[scaleID] = struct{}{}
	}

	unmarshalScaleRequest := func(event *ct.ExpandedEvent) (*api.ScaleRequest, error) {
		var ctReq *ct.ScaleRequest
		if err := json.Unmarshal(event.Data, &ctReq); err != nil {
			return nil, api.NewError(err, err.Error())
		}
		return api.NewScaleRequest(ctReq), nil
	}

	// stream new events as they are created
	var currID int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			event, ok := <-sub.Events
			if !ok {
				break
			}

			// avoid overlap between list and stream
			if event.ID <= currID {
				continue
			}
			currID = event.ID

			if !((req.StreamCreates && event.Op == ct.EventOpCreate) || (req.StreamUpdates && event.Op == ct.EventOpUpdate)) {
				// EventOp doesn't match the stream type
				continue
			}

			scale, err := unmarshalScaleRequest(event)
			if err != nil {
				logger.Error("error unmarshalling event", "rpcMethod", "StreamScales", "event_id", event.ID, "error", err)
				continue
			}

			if len(stateFilterMap) > 0 {
				if _, ok := stateFilterMap[scale.State]; !ok {
					continue
				}
			}

			releaseIDMatches := false
			if len(releaseIDsMap) > 0 {
				if _, ok := releaseIDsMap[api.ParseIDFromName(scale.Name, "releases")]; ok {
					releaseIDMatches = true
				}
			}

			appIDMatches := false
			if len(appIDsMap) > 0 {
				if _, ok := appIDsMap[api.ParseIDFromName(scale.Name, "apps")]; ok {
					appIDMatches = true
				}
			}

			scaleIDMatches := false
			if len(scaleIDsMap) > 0 {
				if _, ok := scaleIDsMap[api.ParseIDFromName(scale.Name, "scales")]; ok {
					scaleIDMatches = true
				}
			}

			if !(releaseIDMatches || appIDMatches || scaleIDMatches) {
				if len(releaseIDsMap) > 0 || len(appIDsMap) > 0 || len(scaleIDsMap) > 0 {
					continue
				}
			}

			stream.Send(&api.StreamScalesResponse{
				ScaleRequests: []*api.ScaleRequest{scale},
			})
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func (g *grpcAPI) StreamReleases(req *api.StreamReleasesRequest, stream api.Controller_StreamReleasesServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)
	pageToken, err := data.ParsePageToken(req.PageToken)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid page token: %q", req.PageToken)
	}
	pageSize := int(req.PageSize)
	if pageSize > 0 {
		pageToken.Size = pageSize
	} else {
		pageSize = pageToken.Size
	}
	appIDs := api.ParseIDsFromNameFilters(req.NameFilters, "apps")
	releaseIDs := api.ParseIDsFromNameFilters(req.NameFilters, "releases")

	sub, err := g.subscribeEvents(&data.EventSubscriptionOpts{
		AppIDs:      appIDs,
		ObjectTypes: []ct.EventType{ct.EventTypeRelease},
		ObjectIDs:   releaseIDs,
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	defer sub.Close()

	// get all releases up until now
	ctReleases, nextPageToken, err := g.releaseRepo.ListPage(data.ListReleaseOptions{
		PageToken:    *pageToken,
		AppIDs:       appIDs,
		ReleaseIDs:   releaseIDs,
		LabelFilters: api.NewControllerLabelFilters(req.GetLabelFilters()),
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	releases := make([]*api.Release, 0, len(ctReleases))
	for _, ctRelease := range ctReleases {
		r := api.NewRelease(ctRelease)
		if !api.MatchLabelFilters(r.Labels, req.GetLabelFilters()) {
			continue
		}
		releases = append(releases, r)
	}

	stream.Send(&api.StreamReleasesResponse{
		Releases:      releases,
		NextPageToken: nextPageToken.String(),
		PageComplete:  true,
	})

	if unary {
		return nil
	}

	unmarshalRelease := func(event *ct.ExpandedEvent) (*api.Release, error) {
		var ctRelease *ct.Release
		if err := json.Unmarshal(event.Data, &ctRelease); err != nil {
			return nil, api.NewError(err, err.Error())
		}
		return api.NewRelease(ctRelease), nil
	}

	maybeAcceptRelease := func(event *ct.ExpandedEvent) (release *api.Release, accepted bool) {
		r, err := unmarshalRelease(event)
		if err != nil {
			logger.Error("error unmarshalling event", "rpcMethod", "StreamReleases", "event_id", event.ID, "error", err)
			return
		}

		if !api.MatchLabelFilters(r.Labels, req.GetLabelFilters()) {
			return
		}

		return r, true
	}

	// stream new events as they are created
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var currID int64
		for {
			event, ok := <-sub.Events
			if !ok {
				break
			}

			// avoid overlap between list and stream
			if event.ID <= currID {
				continue
			}
			currID = event.ID

			if release, ok := maybeAcceptRelease(event); ok {
				stream.Send(&api.StreamReleasesResponse{
					Releases: []*api.Release{release},
				})
			}
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func (g *grpcAPI) CreateRelease(ctx context.Context, req *api.CreateReleaseRequest) (*api.Release, error) {
	ctRelease := req.Release.ControllerType()
	ctRelease.AppID = api.ParseIDFromName(req.Parent, "apps")
	if err := g.releaseRepo.Add(ctRelease); err != nil {
		return nil, api.NewError(err, err.Error())
	}
	return api.NewRelease(ctRelease), nil
}

func (g *grpcAPI) listDeployments(req *api.StreamDeploymentsRequest) ([]*api.ExpandedDeployment, *data.PageToken, error) {
	pageToken, err := data.ParsePageToken(req.PageToken)
	if err != nil {
		return nil, nil, err
	}
	if req.PageSize > 0 {
		pageToken.Size = int(req.PageSize)
	}
	statusFilters := make([]string, 0, len(req.StatusFilters))
	for _, status := range req.StatusFilters {
		statusFilters = append(statusFilters, status.ControllerType())
	}
	typeFilters := make([]ct.ReleaseType, 0, len(req.TypeFilters))
	for _, t := range req.TypeFilters {
		var ctReleaseType ct.ReleaseType
		switch t {
		case api.ReleaseType_CODE:
			ctReleaseType = ct.ReleaseTypeCode
			break
		case api.ReleaseType_CONFIG:
			ctReleaseType = ct.ReleaseTypeConfig
			break
		default:
			ctReleaseType = ct.ReleaseTypeAny
		}
		if ctReleaseType == ct.ReleaseTypeAny {
			typeFilters = []ct.ReleaseType{}
			break
		}
		typeFilters = append(typeFilters, ctReleaseType)
	}
	ctExpandedDeployments, nextPageToken, err := g.deploymentRepo.ListPage(data.ListDeploymentOptions{
		PageToken:     *pageToken,
		AppIDs:        api.ParseIDsFromNameFilters(req.NameFilters, "apps"),
		DeploymentIDs: api.ParseIDsFromNameFilters(req.NameFilters, "deployments"),
		StatusFilters: statusFilters,
		TypeFilters:   typeFilters,
	})
	if err != nil {
		return nil, nil, err
	}

	deployments := make([]*api.ExpandedDeployment, 0, len(ctExpandedDeployments))
	for _, d := range ctExpandedDeployments {
		deployments = append(deployments, api.NewExpandedDeployment(d))
	}
	return deployments, nextPageToken, nil
}

func (g *grpcAPI) StreamDeployments(req *api.StreamDeploymentsRequest, stream api.Controller_StreamDeploymentsServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	appIDs := api.ParseIDsFromNameFilters(req.NameFilters, "apps")
	deploymentIDs := api.ParseIDsFromNameFilters(req.NameFilters, "deployments")

	var deploymentsMtx sync.RWMutex
	var deployments []*api.ExpandedDeployment
	var nextPageToken *data.PageToken
	refreshDeployments := func() error {
		deploymentsMtx.Lock()
		defer deploymentsMtx.Unlock()
		var err error
		deployments, nextPageToken, err = g.listDeployments(req)
		return err
	}

	sendResponse := func() {
		deploymentsMtx.RLock()
		stream.Send(&api.StreamDeploymentsResponse{
			Deployments:   deployments,
			PageComplete:  true,
			NextPageToken: nextPageToken.String(),
		})
		deploymentsMtx.RUnlock()
	}

	if err := refreshDeployments(); err != nil {
		return api.NewError(err, err.Error())
	}
	sendResponse()

	if unary {
		return nil
	}

	var wg sync.WaitGroup

	sub, err := g.subscribeEvents(&data.EventSubscriptionOpts{
		AppIDs:      appIDs,
		ObjectTypes: []ct.EventType{ct.EventTypeDeployment},
		ObjectIDs:   deploymentIDs,
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	defer sub.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		typeMatcher := api.NewReleaseTypeMatcher(req.TypeFilters)
		for {
			event, ok := <-sub.Events
			if !ok {
				break
			}

			if !((req.StreamCreates && event.Op == ct.EventOpCreate) || (req.StreamUpdates && event.Op == ct.EventOpUpdate)) {
				// EventOp doesn't match the stream type
				continue
			}

			var deploymentEvent *ct.DeploymentEvent
			if err := json.Unmarshal(event.Data, &deploymentEvent); err != nil {
				logger.Error("error unmarshalling event", "rpcMethod", "StreamDeployments", "error", err)
				continue
			}
			ctd, err := g.deploymentRepo.GetExpanded(event.ObjectID)
			if err != nil {
				logger.Error("error fetching deployment for event", "rpcMethod", "StreamDeployments", "deployment_id", event.ObjectID, "error", err)
				continue
			}
			ctd.Status = deploymentEvent.Status
			d := api.NewExpandedDeployment(ctd)

			if !typeMatcher.Match(d.Type) {
				continue
			}

			if len(req.StatusFilters) > 0 {
				statusMatched := false
				for _, status := range req.StatusFilters {
					if status == d.Status {
						statusMatched = true
						break
					}
				}
				if !statusMatched {
					continue
				}
			}

			stream.Send(&api.StreamDeploymentsResponse{
				Deployments: []*api.ExpandedDeployment{d},
			})
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func (g *grpcAPI) listDeploymentEvents(req *api.StreamDeploymentEventsRequest) ([]*api.Event, *data.PageToken, error) {
	pageSize := int(req.GetPageSize())
	pageToken, err := data.ParsePageToken(req.PageToken)
	if err != nil {
		return nil, nil, err
	}

	if pageSize > 0 {
		pageToken.Size = pageSize
	} else {
		pageSize = pageToken.Size
	}

	appIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
	deploymentIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "deployments")
	objectTypes := api.ParseEventTypeFilters(req.GetTypeFilters())
	if len(objectTypes) == 0 {
		objectTypes = []ct.EventType{ct.EventTypeDeployment, ct.EventTypeScaleRequest, ct.EventTypeJob}
	}
	ctEvents, nextPageToken, err := g.eventRepo.ListPage(data.ListEventOptions{
		PageToken:     *pageToken,
		AppIDs:        appIDs,
		ObjectTypes:   objectTypes,
		DeploymentIDs: deploymentIDs,
	})
	if err != nil {
		return nil, nil, err
	}

	events := make([]*api.Event, len(ctEvents))
	for i, e := range ctEvents {
		events[i] = api.NewEvent(e)
	}

	return events, nextPageToken, nil
}

func (g *grpcAPI) StreamDeploymentEvents(req *api.StreamDeploymentEventsRequest, stream api.Controller_StreamDeploymentEventsServer) (err error) {
	defer (func() {
		if err != nil {
			err = api.NewError(err, err.Error())
		}
	})()

	unary := !req.StreamCreates

	var sub *data.EventSubscriber
	if !unary {
		appIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
		deploymentIDs := api.ParseIDsFromNameFilters(req.GetNameFilters(), "deployments")
		objectTypes := api.ParseEventTypeFilters(req.GetTypeFilters())
		if len(objectTypes) == 0 {
			objectTypes = []ct.EventType{ct.EventTypeDeployment, ct.EventTypeScaleRequest, ct.EventTypeJob}
		}
		sub, err = g.subscribeEvents(&data.EventSubscriptionOpts{
			AppIDs:        appIDs,
			ObjectTypes:   objectTypes,
			DeploymentIDs: deploymentIDs,
		})
		if err != nil {
			return
		}
		defer sub.Close()
	}

	events, nextPageToken, err := g.listDeploymentEvents(req)
	if err != nil {
		return
	}

	stream.Send(&api.StreamDeploymentEventsResponse{
		Events:        events,
		NextPageToken: nextPageToken.String(),
		PageComplete:  true,
	})

	if sub == nil {
		return
	}

	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				err = sub.Err
				return
			}

			stream.Send(&api.StreamDeploymentEventsResponse{
				Events: []*api.Event{api.NewEvent(event)},
			})
		case <-stream.Context().Done():
			err = stream.Context().Err()
			return
		}
	}
}

func parseDeploymentTags(from map[string]*api.DeploymentProcessTags) map[string]map[string]string {
	to := make(map[string]map[string]string, len(from))
	for k, v := range from {
		to[k] = v.Tags
	}
	return to
}

func parseDeploymentProcesses(from map[string]int32) map[string]int {
	to := make(map[string]int, len(from))
	for k, v := range from {
		to[k] = int(v)
	}
	return to
}

func (g *grpcAPI) CreateDeployment(req *api.CreateDeploymentRequest, ds api.Controller_CreateDeploymentServer) error {
	d, err := g.deploymentRepo.AddExpanded(req.ControllerType())
	if err != nil {
		return api.NewError(err, err.Error())
	}

	// handle deployments without processes
	if api.NewDeploymentStatus(d.Status) == api.DeploymentStatus_COMPLETE {
		return nil
	}

	// Wait for deployment to complete and perform scale

	sub, err := g.subscribeEvents(&data.EventSubscriptionOpts{
		AppIDs:        []string{d.AppID},
		ObjectTypes:   []ct.EventType{ct.EventTypeDeployment, ct.EventTypeJob},
		DeploymentIDs: []string{d.ID},
	})
	if err != nil {
		return api.NewError(err, err.Error())
	}
	defer sub.Close()

outer:
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				break outer
			}
			if event.ObjectType != "deployment" && event.ObjectType != "job" {
				continue outer
			}

			var deploymentID string
			if event.Deployment != nil {
				deploymentID = event.Deployment.ID
			}
			var de *ct.DeploymentEvent
			switch event.ObjectType {
			case "deployment":
				if err := json.Unmarshal(event.Data, &de); err != nil {
					logger.Error("failed to unmarshal deployment event", "event_id", event.ID, "deployment_id", event.ObjectID, "error", err)
					continue outer
				}

			case "job":
				var job *ct.Job
				if err := json.Unmarshal(event.Data, &job); err != nil {
					logger.Error("failed to unmarshal job event", "event_id", event.ID, "deployment_id", deploymentID, "job_uuid", event.UniqueID, "error", err)
					continue outer
				}
			}
			ds.Send(api.NewEvent(event))

			if de != nil {
				if de.Status == "failed" {
					return status.Errorf(codes.FailedPrecondition, de.Error)
				}
				if de.Status == "complete" {
					break outer
				}
			}
		case <-ds.Context().Done():
			err := ds.Context().Err()
			return maybeError(err)
		}
	}

	return maybeError(sub.Err)
}

func maybeError(err error) error {
	if err == nil {
		return nil
	}
	return api.NewError(err, err.Error())
}
