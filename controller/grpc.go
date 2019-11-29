package main

import (
	"encoding/json"
	fmt "fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/data"
	"github.com/flynn/flynn/controller/grpc/protobuf"
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

const ctxKeyFlynnAuthKeyID = "flynn-auth-key-id"

func (g *grpcAPI) authorize(ctx context.Context) (context.Context, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if passwords, ok := md["auth-key"]; ok && len(passwords) > 0 {
			auth, err := g.authorizer.Authorize(passwords[0])
			if err != nil {
				return ctx, grpc.Errorf(codes.Unauthenticated, err.Error())
			}

			if auth.ID != "" {
				ctx = context.WithValue(ctx, ctxKeyFlynnAuthKeyID, auth.ID)
				ctx = ctxhelper.NewContextLogger(ctx, g.logger(ctx).New("authKeyID", auth.ID))
			}

			return ctx, nil
		}

		return ctx, grpc.Errorf(codes.Unauthenticated, "no Auth-Key provided")
	}

	return ctx, grpc.Errorf(codes.Unauthenticated, "metadata missing")
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
		duration := time.Since(startTime)
		if err == nil {
			g.logger(ctx).Info("gRPC request ended", "duration", duration)
		} else {
			g.logger(ctx).Info("gRPC request ended", "duration", duration, "err", err)
		}
	}
}

type grpcEventListener struct {
	Events  chan *ct.Event
	Err     error
	errOnce sync.Once
	subs    []*data.EventSubscriber
}

func (e *grpcEventListener) Close() {
	for _, sub := range e.subs {
		sub.Close()
		if err := sub.Err; err != nil {
			e.errOnce.Do(func() { e.Err = err })
		}
	}
}

func (g *grpcAPI) subscribeEvents(appIDs []string, objectTypes []ct.EventType, objectIDs []string) (*grpcEventListener, error) {
	dataEventListener, err := g.maybeStartEventListener()
	if err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}

	eventListener := &grpcEventListener{
		Events: make(chan *ct.Event),
	}

	objectTypeStrings := make([]string, len(objectTypes))
	for i, t := range objectTypes {
		objectTypeStrings[i] = string(t)
	}

	if len(appIDs) == 0 && len(objectIDs) == 0 {
		// an empty string matches all app ids
		appIDs = []string{""}
	}
	subs := make([]*data.EventSubscriber, 0, len(appIDs)+len(objectIDs))
	for _, appID := range appIDs {
		sub, err := dataEventListener.Subscribe(appID, objectTypeStrings, "")
		if err != nil {
			return nil, protobuf.NewError(err, err.Error())
		}
		subs = append(subs, sub)
		go (func() {
			for {
				event, ok := <-sub.Events
				if !ok {
					break
				}
				eventListener.Events <- event
			}
		})()
	}
	for _, objectID := range objectIDs {
		sub, err := dataEventListener.Subscribe("", objectTypeStrings, objectID)
		if err != nil {
			return nil, protobuf.NewError(err, err.Error())
		}
		subs = append(subs, sub)
		go (func() {
			for {
				event, ok := <-sub.Events
				if !ok {
					break
				}
				eventListener.Events <- event
			}
		})()
	}
	eventListener.subs = subs
	return eventListener, nil
}

func (g *grpcAPI) grpcServer() *grpc.Server {
	s := grpc.NewServer(
		grpc.StatsHandler(&grpcStatsHandler{}),
		grpc.StreamInterceptor(g.streamInterceptor),
		grpc.UnaryInterceptor(g.unaryInterceptor),
	)
	protobuf.RegisterControllerServer(s, g)
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
	var logRequestEnd func(context.Context, error)
	ctx, logRequestEnd = g.logRequest(ctx, info.FullMethod)
	defer func() {
		logRequestEnd(ctx, err)
	}()

	ctx, err = g.authorize(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

func (g *grpcAPI) Status(context.Context, *empty.Empty) (*protobuf.StatusResponse, error) {
	healthy := true
	if err := g.db.Exec("ping"); err != nil {
		healthy = false
	}
	return protobuf.NewStatusResponse(healthy, nil), nil
}

func (g *grpcAPI) listApps(req *protobuf.StreamAppsRequest) ([]*protobuf.App, *data.PageToken, error) {
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

	appIDs := protobuf.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
	ctApps, nextPageToken, err := g.appRepo.ListPage(data.ListAppOptions{
		PageToken:    *pageToken,
		AppIDs:       appIDs,
		LabelFilters: protobuf.NewControllerLabelFilters(req.GetLabelFilters()),
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

	apps := make([]*protobuf.App, 0, pageSize)
	n := 0
	for _, a := range ctApps {
		apps = append(apps, protobuf.NewApp(a))
		n++

		if n == pageSize {
			break
		}
	}

	return apps, nextPageToken, nil
}

func (g *grpcAPI) StreamApps(req *protobuf.StreamAppsRequest, stream protobuf.Controller_StreamAppsServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	var apps []*protobuf.App
	var nextPageToken *data.PageToken
	var appsMtx sync.RWMutex
	refreshApps := func() error {
		appsMtx.Lock()
		defer appsMtx.Unlock()
		var err error
		apps, nextPageToken, err = g.listApps(req)
		return err
	}

	sendResponse := func() {
		appsMtx.RLock()
		stream.Send(&protobuf.StreamAppsResponse{
			Apps:          apps,
			NextPageToken: nextPageToken.String(),
			PageComplete:  true,
		})
		appsMtx.RUnlock()
	}

	var sub *grpcEventListener
	var err error
	if !unary {
		appIDs := protobuf.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
		sub, err = g.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeApp, ct.EventTypeAppDeletion, ct.EventTypeAppRelease}, nil)
		if err != nil {
			return protobuf.NewError(err, err.Error())
		}
		defer sub.Close()
	}

	if err := refreshApps(); err != nil {
		return protobuf.NewError(err, err.Error())
	}
	sendResponse()
	if unary {
		return nil
	}

	maybeSendApp := func(event *ct.Event, app *protobuf.App) {
		shouldSend := false
		if (req.StreamCreates && event.Op == ct.EventOpCreate) || (req.StreamUpdates && event.Op == ct.EventOpUpdate) || (req.StreamUpdates && event.ObjectType == ct.EventTypeAppRelease) {
			shouldSend = protobuf.MatchLabelFilters(app.Labels, req.GetLabelFilters())
		}
		if shouldSend {
			stream.Send(&protobuf.StreamAppsResponse{
				Apps: []*protobuf.App{app},
			})
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			event, ok := <-sub.Events
			if !ok {
				break
			}
			switch event.ObjectType {
			case ct.EventTypeApp:
				var ctApp *ct.App
				if err := json.Unmarshal(event.Data, &ctApp); err != nil {
					logger.Error("error unmarshalling event", "rpcMethod", "StreamApps", "event_id", event.ID, "error", err)
					continue
				}
				maybeSendApp(event, protobuf.NewApp(ctApp))
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
				app := protobuf.NewApp(&ct.App{ID: ctAppDeletionEvent.AppDeletion.AppID})
				app.DeleteTime = protobuf.NewTimestamp(event.CreatedAt)
				stream.Send(&protobuf.StreamAppsResponse{
					Apps: []*protobuf.App{app},
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
				maybeSendApp(event, protobuf.NewApp(ctApp.(*ct.App)))
			}
		}
	}()
	wg.Wait()

	if err := sub.Err; err != nil {
		return protobuf.NewError(err, err.Error())
	}

	return nil
}

func (g *grpcAPI) UpdateApp(ctx context.Context, req *protobuf.UpdateAppRequest) (*protobuf.App, error) {
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

	ctApp, err := g.appRepo.Update(protobuf.ParseIDFromName(app.Name, "apps"), data)
	if err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}
	return protobuf.NewApp(ctApp.(*ct.App)), nil
}

func (g *grpcAPI) createScale(req *protobuf.CreateScaleRequest) (*protobuf.ScaleRequest, error) {
	appID := protobuf.ParseIDFromName(req.Parent, "apps")
	releaseID := protobuf.ParseIDFromName(req.Parent, "releases")
	processes := parseDeploymentProcesses(req.Processes)
	tags := parseDeploymentTags(req.Tags)

	sub, err := g.subscribeEvents([]string{appID}, []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation}, nil)
	if err != nil {
		return nil, protobuf.NewError(err, err.Error())
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
		return nil, protobuf.NewError(err, err.Error())
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
		return nil, protobuf.NewError(err, err.Error())
	}

	return protobuf.NewScaleRequest(scaleReq), nil
}

func (g *grpcAPI) CreateScale(ctx context.Context, req *protobuf.CreateScaleRequest) (*protobuf.ScaleRequest, error) {
	return g.createScale(req)
}

func (g *grpcAPI) StreamScales(req *protobuf.StreamScalesRequest, stream protobuf.Controller_StreamScalesServer) error {
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

	appIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "apps")
	releaseIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "releases")
	scaleIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "scales")

	streamAppIDs := appIDs
	streamScaleIDs := scaleIDs
	if len(releaseIDs) > 0 {
		// we can't filter releaseIDs in the subscription, so don't filter anything
		streamAppIDs = nil
		streamScaleIDs = nil
	}
	sub, err := g.subscribeEvents(streamAppIDs, []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation}, streamScaleIDs)
	if err != nil {
		return protobuf.NewError(err, err.Error())
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
		return protobuf.NewError(err, err.Error())
	}
	scaleRequests := make([]*protobuf.ScaleRequest, 0, len(list))
	for _, ctScale := range list {
		scaleRequests = append(scaleRequests, protobuf.NewScaleRequest(ctScale))
	}
	stream.Send(&protobuf.StreamScalesResponse{
		ScaleRequests: scaleRequests,
		PageComplete:  true,
		NextPageToken: nextPageToken.String(),
	})

	if unary {
		return nil
	}

	stateFilterMap := make(map[protobuf.ScaleRequestState]struct{}, len(req.StateFilters))
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

	unmarshalScaleRequest := func(event *ct.Event) (*protobuf.ScaleRequest, error) {
		var ctReq *ct.ScaleRequest
		if err := json.Unmarshal(event.Data, &ctReq); err != nil {
			return nil, protobuf.NewError(err, err.Error())
		}
		return protobuf.NewScaleRequest(ctReq), nil
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
				if _, ok := releaseIDsMap[protobuf.ParseIDFromName(scale.Name, "releases")]; ok {
					releaseIDMatches = true
				}
			}

			appIDMatches := false
			if len(appIDsMap) > 0 {
				if _, ok := appIDsMap[protobuf.ParseIDFromName(scale.Name, "apps")]; ok {
					appIDMatches = true
				}
			}

			scaleIDMatches := false
			if len(scaleIDsMap) > 0 {
				if _, ok := scaleIDsMap[protobuf.ParseIDFromName(scale.Name, "scales")]; ok {
					scaleIDMatches = true
				}
			}

			if !(releaseIDMatches || appIDMatches || scaleIDMatches) {
				if len(releaseIDsMap) > 0 || len(appIDsMap) > 0 || len(scaleIDsMap) > 0 {
					continue
				}
			}

			stream.Send(&protobuf.StreamScalesResponse{
				ScaleRequests: []*protobuf.ScaleRequest{scale},
			})
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func (g *grpcAPI) StreamReleases(req *protobuf.StreamReleasesRequest, stream protobuf.Controller_StreamReleasesServer) error {
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
	appIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "apps")
	releaseIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "releases")

	sub, err := g.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeRelease}, releaseIDs)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	defer sub.Close()

	// get all releases up until now
	ctReleases, nextPageToken, err := g.releaseRepo.ListPage(data.ListReleaseOptions{
		PageToken:    *pageToken,
		AppIDs:       appIDs,
		ReleaseIDs:   releaseIDs,
		LabelFilters: protobuf.NewControllerLabelFilters(req.GetLabelFilters()),
	})
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	releases := make([]*protobuf.Release, 0, len(ctReleases))
	for _, ctRelease := range ctReleases {
		r := protobuf.NewRelease(ctRelease)
		if !protobuf.MatchLabelFilters(r.Labels, req.GetLabelFilters()) {
			continue
		}
		releases = append(releases, r)
	}

	stream.Send(&protobuf.StreamReleasesResponse{
		Releases:      releases,
		NextPageToken: nextPageToken.String(),
		PageComplete:  true,
	})

	if unary {
		return nil
	}

	unmarshalRelease := func(event *ct.Event) (*protobuf.Release, error) {
		var ctRelease *ct.Release
		if err := json.Unmarshal(event.Data, &ctRelease); err != nil {
			return nil, protobuf.NewError(err, err.Error())
		}
		return protobuf.NewRelease(ctRelease), nil
	}

	maybeAcceptRelease := func(event *ct.Event) (release *protobuf.Release, accepted bool) {
		r, err := unmarshalRelease(event)
		if err != nil {
			logger.Error("error unmarshalling event", "rpcMethod", "StreamReleases", "event_id", event.ID, "error", err)
			return
		}

		if !protobuf.MatchLabelFilters(r.Labels, req.GetLabelFilters()) {
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
				stream.Send(&protobuf.StreamReleasesResponse{
					Releases: []*protobuf.Release{release},
				})
			}
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func (g *grpcAPI) CreateRelease(ctx context.Context, req *protobuf.CreateReleaseRequest) (*protobuf.Release, error) {
	ctRelease := req.Release.ControllerType()
	ctRelease.AppID = protobuf.ParseIDFromName(req.Parent, "apps")
	if err := g.releaseRepo.Add(ctRelease); err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}
	return protobuf.NewRelease(ctRelease), nil
}

func (g *grpcAPI) listDeployments(req *protobuf.StreamDeploymentsRequest) ([]*protobuf.ExpandedDeployment, *data.PageToken, error) {
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
		case protobuf.ReleaseType_CODE:
			ctReleaseType = ct.ReleaseTypeCode
			break
		case protobuf.ReleaseType_CONFIG:
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
		AppIDs:        protobuf.ParseIDsFromNameFilters(req.NameFilters, "apps"),
		DeploymentIDs: protobuf.ParseIDsFromNameFilters(req.NameFilters, "deployments"),
		StatusFilters: statusFilters,
		TypeFilters:   typeFilters,
	})
	if err != nil {
		return nil, nil, err
	}

	deployments := make([]*protobuf.ExpandedDeployment, 0, len(ctExpandedDeployments))
	for _, d := range ctExpandedDeployments {
		deployments = append(deployments, protobuf.NewExpandedDeployment(d))
	}
	return deployments, nextPageToken, nil
}

func (g *grpcAPI) StreamDeployments(req *protobuf.StreamDeploymentsRequest, stream protobuf.Controller_StreamDeploymentsServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	appIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "apps")
	deploymentIDs := protobuf.ParseIDsFromNameFilters(req.NameFilters, "deployments")

	var deploymentsMtx sync.RWMutex
	var deployments []*protobuf.ExpandedDeployment
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
		stream.Send(&protobuf.StreamDeploymentsResponse{
			Deployments:   deployments,
			PageComplete:  true,
			NextPageToken: nextPageToken.String(),
		})
		deploymentsMtx.RUnlock()
	}

	if err := refreshDeployments(); err != nil {
		return protobuf.NewError(err, err.Error())
	}
	sendResponse()

	if unary {
		return nil
	}

	var wg sync.WaitGroup

	sub, err := g.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeDeployment}, deploymentIDs)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	defer sub.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		typeMatcher := protobuf.NewReleaseTypeMatcher(req.TypeFilters)
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
			d := protobuf.NewExpandedDeployment(ctd)

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

			stream.Send(&protobuf.StreamDeploymentsResponse{
				Deployments: []*protobuf.ExpandedDeployment{d},
			})
		}
	}()
	wg.Wait()

	return maybeError(sub.Err)
}

func parseDeploymentTags(from map[string]*protobuf.DeploymentProcessTags) map[string]map[string]string {
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

func (g *grpcAPI) CreateDeployment(req *protobuf.CreateDeploymentRequest, ds protobuf.Controller_CreateDeploymentServer) error {
	appID := protobuf.ParseIDFromName(req.Parent, "apps")
	releaseID := protobuf.ParseIDFromName(req.Parent, "releases")
	d, err := g.deploymentRepo.Add(appID, releaseID)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}

	// Wait for deployment to complete and perform scale

	sub, err := g.subscribeEvents([]string{appID}, []ct.EventType{ct.EventTypeDeployment}, []string{d.ID})
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	defer sub.Close()

	for {
		event, ok := <-sub.Events
		if !ok {
			break
		}
		if event.ObjectType != "deployment" {
			continue
		}
		var de *ct.DeploymentEvent
		if err := json.Unmarshal(event.Data, &de); err != nil {
			logger.Error("failed to unmarshal deployment event", "event_id", event.ID, "deployment_id", event.ObjectID, "error", err)
			continue
		}

		// Scale release to requested processes/tags once deployment is complete
		if de.Status == "complete" {
			if sr := req.ScaleRequest; sr != nil {
				if _, err := g.createScale(&protobuf.CreateScaleRequest{
					Parent:    fmt.Sprintf("apps/%s/releases/%s", de.AppID, de.ReleaseID),
					Processes: sr.Processes,
					Tags:      sr.Tags,
				}); err != nil {
					return err
				}
			}
		}

		ds.Send(&protobuf.DeploymentEvent{
			Parent:     fmt.Sprintf("apps/%s/deployments/%s", de.AppID, de.DeploymentID),
			JobType:    de.JobType,
			JobState:   protobuf.NewJobState(de.JobState),
			Error:      de.Error,
			CreateTime: protobuf.NewTimestamp(event.CreatedAt),
		})

		if de.Status == "failed" {
			return status.Errorf(codes.FailedPrecondition, de.Error)
		}
		if de.Status == "complete" {
			break
		}
	}

	return maybeError(sub.Err)
}

func maybeError(err error) error {
	if err == nil {
		return nil
	}
	return protobuf.NewError(err, err.Error())
}
