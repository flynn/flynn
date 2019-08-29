package main

import (
	"encoding/json"
	fmt "fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/data"
	"github.com/flynn/flynn/controller/grpc/protobuf"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	flynnstatus "github.com/flynn/flynn/pkg/status"
	routerc "github.com/flynn/flynn/router/client"
	que "github.com/flynn/que-go"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/improbable-eng/grpc-web/go/grpcweb"
	log "github.com/inconshreveable/log15"
	"github.com/soheilhy/cmux"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

func mustEnv(key string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	shutdown.Fatalf("%s is required", key)
	return ""
}

var logger = log.New("component", "controller/grpc")

var schemaRoot = "/etc/flynn-controller/jsonschema"

func main() {
	logger.Debug("opening database connection...")

	// Open connection to main controller database
	db := postgres.Wait(nil, data.PrepareStatements)
	shutdown.BeforeExit(func() { db.Close() })
	q := que.NewClient(db.ConnPool)

	logger.Debug("initializing server...")
	config := configureRepos(&Config{
		logger:     logger,
		DB:         db,
		q:          q,
		authorizer: utils.NewAuthorizer(strings.Split(os.Getenv("AUTH_KEY"), ","), strings.Split(os.Getenv("AUTH_KEY_IDS"), ",")),
	})
	s := NewServer(config)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port
	logger.Debug(fmt.Sprintf("attempting to listen on %q...", addr))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Debug(fmt.Sprintf("error opening listener on %q...: %v", addr, err))
		shutdown.Fatalf("failed to create listener: %v", err)
	}
	logger.Debug("listener created")
	shutdown.BeforeExit(func() { l.Close() })
	runServer(s, config, l)
	logger.Debug("servers stopped")
}

func runServer(s *grpc.Server, c *Config, l net.Listener) {
	logger.Debug("loading JSON schemas...")

	if err := schema.Load(schemaRoot); err != nil {
		shutdown.Fatal(err)
	}

	logger.Debug("initializing grpc-web server...")
	grpcWebServer := grpcweb.WrapServer(s)

	logger.Debug("initializing cmux listeners...")
	m := cmux.New(l)
	grpcListener := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	grpcWebListener := m.Match(cmux.Any())

	statusHandler := flynnstatus.Handler(func() flynnstatus.Status {
		if err := c.DB.Exec("ping"); err != nil {
			return flynnstatus.Unhealthy
		}
		return flynnstatus.Healthy
	})

	var wg sync.WaitGroup

	logger.Debug("starting servers...")
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Debug("starting gRPC server...")
		s.Serve(grpcListener)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Debug("starting gRPC-web server...")
		http.Serve(
			grpcWebListener,
			httphelper.ContextInjector(
				"controller/grpc [gRPC-web]",
				httphelper.NewRequestLogger(corsHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.URL.Path == flynnstatus.Path {
						statusHandler.ServeHTTP(w, req)
						return
					}

					grpcWebServer.ServeHTTP(w, req)
				}))),
			),
		)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Debug("starting mux server...")
		m.Serve()
	}()

	wg.Wait()
}

type Config struct {
	logger           log.Logger
	DB               *postgres.DB
	q                *que.Client
	appRepo          *data.AppRepo
	artifactRepo     *data.ArtifactRepo
	releaseRepo      *data.ReleaseRepo
	formationRepo    *data.FormationRepo
	deploymentRepo   *data.DeploymentRepo
	eventRepo        *data.EventRepo
	eventListenerMtx sync.Mutex
	eventListener    *data.EventListener
	authorizer       *utils.Authorizer
}

func configureRepos(c *Config) *Config {
	c.appRepo = data.NewAppRepo(c.DB, os.Getenv("DEFAULT_ROUTE_DOMAIN"), routerc.New())
	c.artifactRepo = data.NewArtifactRepo(c.DB)
	c.releaseRepo = data.NewReleaseRepo(c.DB, c.artifactRepo, c.q)
	c.formationRepo = data.NewFormationRepo(c.DB, c.appRepo, c.releaseRepo, c.artifactRepo)
	c.eventRepo = data.NewEventRepo(c.DB)
	c.deploymentRepo = data.NewDeploymentRepo(c.DB, c.appRepo, c.releaseRepo, c.formationRepo)
	return c
}

const ctxKeyFlynnAuthKeyID = "flynn-auth-key-id"

func (c *Config) Authorize(ctx context.Context) (context.Context, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if passwords, ok := md["auth-key"]; ok && len(passwords) > 0 {
			auth, err := c.authorizer.Authorize(passwords[0])
			if err != nil {
				return ctx, grpc.Errorf(codes.Unauthenticated, err.Error())
			}

			if auth.ID != "" {
				ctx = context.WithValue(ctx, ctxKeyFlynnAuthKeyID, auth.ID)

				logger, ok := ctxhelper.LoggerFromContext(ctx)
				if !ok {
					logger = c.logger
				}
				ctx = ctxhelper.NewContextLogger(ctx, logger.New("authKeyID", auth.ID))
			}

			return ctx, nil
		}

		return ctx, grpc.Errorf(codes.Unauthenticated, "no Auth-Key provided")
	}

	return ctx, grpc.Errorf(codes.Unauthenticated, "metadata missing")
}

func (c *Config) Logger(ctx context.Context) log.Logger {
	logger, ok := ctxhelper.LoggerFromContext(ctx)
	if !ok {
		logger = c.logger
	}
	return logger
}

func (c *Config) LogRequest(ctx context.Context, rpcMethod string) (context.Context, func(context.Context, error)) {
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
		ctx = ctxhelper.NewContextLogger(ctx, c.Logger(ctx).New("req_id", reqID[0]))

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

	c.Logger(ctx).Info("gRPC request started", "rpcMethod", rpcMethod, "client_ip", clientIP)
	return ctx, func(ctx context.Context, err error) {
		duration := time.Since(startTime)
		if err == nil {
			c.Logger(ctx).Info("gRPC request ended", "duration", duration)
		} else {
			c.Logger(ctx).Info("gRPC request ended", "duration", duration, "error", err)
		}
	}
}

func (c *Config) maybeStartEventListener() (*data.EventListener, error) {
	c.eventListenerMtx.Lock()
	defer c.eventListenerMtx.Unlock()
	if c.eventListener != nil && !c.eventListener.IsClosed() {
		return c.eventListener, nil
	}
	c.eventListener = data.NewEventListener(c.eventRepo)
	return c.eventListener, c.eventListener.Listen()
}

type EventListener struct {
	Events  chan *ct.Event
	Err     error
	errOnce sync.Once
	subs    []*data.EventSubscriber
}

func (e *EventListener) Close() {
	for _, sub := range e.subs {
		sub.Close()
		if err := sub.Err; err != nil {
			e.errOnce.Do(func() { e.Err = err })
		}
	}
}

func (c *Config) subscribeEvents(appIDs []string, objectTypes []ct.EventType, objectIDs []string) (*EventListener, error) {
	dataEventListener, err := c.maybeStartEventListener()
	if err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}

	eventListener := &EventListener{
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

func corsHandler(main http.Handler) http.Handler {
	return (&cors.Options{
		ShouldAllowOrigin: func(origin string, req *http.Request) bool {
			return true
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
		AllowHeaders:     []string{"Auth-Key", "Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match", "X-GRPC-Web"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}).Handler(main)
}

func NewServer(c *Config) *grpc.Server {
	s := grpc.NewServer(
		grpc.StatsHandler(&statsHandler{}),
		grpc.StreamInterceptor(streamInterceptor(c)),
		grpc.UnaryInterceptor(unaryInterceptor(c)),
	)
	protobuf.RegisterControllerServer(s, &server{Config: c})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	return s
}

type statsHandler struct{}

func (s *statsHandler) TagRPC(ctx context.Context, rpcTagInfo *stats.RPCTagInfo) context.Context {
	return ctx
}

func (s *statsHandler) HandleRPC(context.Context, stats.RPCStats) {}

const ctxKeyRemoteHost = "remote-host"

func (s *statsHandler) TagConn(ctx context.Context, connTagInfo *stats.ConnTagInfo) context.Context {
	remoteHost, _, _ := net.SplitHostPort(connTagInfo.RemoteAddr.String())
	ctx = context.WithValue(ctx, ctxKeyRemoteHost, remoteHost)
	return ctx
}

func (s *statsHandler) HandleConn(context.Context, stats.ConnStats) {}

func streamInterceptor(c *Config) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx, logRequestEnd := c.LogRequest(stream.Context(), info.FullMethod)
		defer func() {
			logRequestEnd(ctx, err)
		}()

		ctx, err = c.Authorize(stream.Context())
		if err != nil {
			return err
		}

		if l, ok := ctxhelper.LoggerFromContext(ctx); ok {
			logger = l
		}

		wrappedStream := middleware.WrapServerStream(stream)
		wrappedStream.WrappedContext = ctx
		return handler(srv, wrappedStream)
	}
}

func unaryInterceptor(c *Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (res interface{}, err error) {
		var logRequestEnd func(context.Context, error)
		ctx, logRequestEnd = c.LogRequest(ctx, info.FullMethod)
		defer func() {
			logRequestEnd(ctx, err)
		}()

		ctx, err = c.Authorize(ctx)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

type server struct {
	protobuf.ControllerServer
	*Config
}

func (s *server) listApps(req *protobuf.StreamAppsRequest) ([]*protobuf.App, *data.PageToken, error) {
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
	ctApps, nextPageToken, err := s.appRepo.ListPage(data.ListAppOptions{
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

func (s *server) StreamApps(req *protobuf.StreamAppsRequest, stream protobuf.Controller_StreamAppsServer) error {
	unary := !(req.StreamUpdates || req.StreamCreates)

	var apps []*protobuf.App
	var nextPageToken *data.PageToken
	var appsMtx sync.RWMutex
	refreshApps := func() error {
		appsMtx.Lock()
		defer appsMtx.Unlock()
		var err error
		apps, nextPageToken, err = s.listApps(req)
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

	var sub *EventListener
	var err error
	if !unary {
		appIDs := protobuf.ParseIDsFromNameFilters(req.GetNameFilters(), "apps")
		sub, err = s.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeApp, ct.EventTypeAppDeletion, ct.EventTypeAppRelease}, nil)
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
					s.logger.Error("error unmarshalling event", "rpcMethod", "StreamApps", "event_id", event.ID, "error", err)
					continue
				}
				maybeSendApp(event, protobuf.NewApp(ctApp))
			case ct.EventTypeAppDeletion:
				if !req.StreamUpdates {
					continue
				}
				var ctAppDeletionEvent *ct.AppDeletionEvent
				if err := json.Unmarshal(event.Data, &ctAppDeletionEvent); err != nil {
					s.logger.Error("error unmarshalling app deletion event", "rpcMethod", "StreamApps", "event_id", event.ID, "error", err)
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
				ctApp, err := s.appRepo.Get(event.AppID)
				if err != nil {
					s.logger.Error("error fetching app", "rpcMethod", "StreamApps", "app_id", event.AppID, "error", err)
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

func (s *server) UpdateApp(ctx context.Context, req *protobuf.UpdateAppRequest) (*protobuf.App, error) {
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

	ctApp, err := s.appRepo.Update(protobuf.ParseIDFromName(app.Name, "apps"), data)
	if err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}
	return protobuf.NewApp(ctApp.(*ct.App)), nil
}

func (s *server) createScale(req *protobuf.CreateScaleRequest) (*protobuf.ScaleRequest, error) {
	appID := protobuf.ParseIDFromName(req.Parent, "apps")
	releaseID := protobuf.ParseIDFromName(req.Parent, "releases")
	processes := parseDeploymentProcesses(req.Processes)
	tags := parseDeploymentTags(req.Tags)

	sub, err := s.subscribeEvents([]string{appID}, []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation}, nil)
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
	if _, err := s.formationRepo.AddScaleRequest(scaleReq, false); err != nil {
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

func (s *server) CreateScale(ctx context.Context, req *protobuf.CreateScaleRequest) (*protobuf.ScaleRequest, error) {
	return s.createScale(req)
}

func (s *server) StreamScales(req *protobuf.StreamScalesRequest, stream protobuf.Controller_StreamScalesServer) error {
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
	sub, err := s.subscribeEvents(streamAppIDs, []ct.EventType{ct.EventTypeScaleRequest, ct.EventTypeScaleRequestCancelation}, streamScaleIDs)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	defer sub.Close()

	// get all events up until now
	stateFilters := make([]ct.ScaleRequestState, 0, len(req.StateFilters))
	for _, state := range req.StateFilters {
		stateFilters = append(stateFilters, state.ControllerType())
	}
	list, nextPageToken, err := s.formationRepo.ListScaleRequests(data.ListScaleRequestOptions{
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
				s.logger.Error("error unmarshalling event", "rpcMethod", "StreamScales", "event_id", event.ID, "error", err)
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

func (s *server) StreamReleases(req *protobuf.StreamReleasesRequest, stream protobuf.Controller_StreamReleasesServer) error {
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

	sub, err := s.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeRelease}, releaseIDs)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}
	defer sub.Close()

	// get all releases up until now
	ctReleases, nextPageToken, err := s.releaseRepo.ListPage(data.ListReleaseOptions{
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
			s.logger.Error("error unmarshalling event", "rpcMethod", "StreamReleases", "event_id", event.ID, "error", err)
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

func (s *server) CreateRelease(ctx context.Context, req *protobuf.CreateReleaseRequest) (*protobuf.Release, error) {
	ctRelease := req.Release.ControllerType()
	ctRelease.AppID = protobuf.ParseIDFromName(req.Parent, "apps")
	if err := s.releaseRepo.Add(ctRelease); err != nil {
		return nil, protobuf.NewError(err, err.Error())
	}
	return protobuf.NewRelease(ctRelease), nil
}

func (s *server) listDeployments(req *protobuf.StreamDeploymentsRequest) ([]*protobuf.ExpandedDeployment, *data.PageToken, error) {
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
	ctExpandedDeployments, nextPageToken, err := s.deploymentRepo.ListPage(data.ListDeploymentOptions{
		PageToken:     *pageToken,
		AppIDs:        protobuf.ParseIDsFromNameFilters(req.NameFilters, "apps"),
		DeploymentIDs: protobuf.ParseIDsFromNameFilters(req.NameFilters, "deployments"),
		StatusFilters: statusFilters,
	})
	if err != nil {
		return nil, nil, err
	}

	deployments := make([]*protobuf.ExpandedDeployment, 0, len(ctExpandedDeployments))
	for _, d := range ctExpandedDeployments {
		deployments = append(deployments, protobuf.NewExpandedDeployment(d))
	}

	var filtered []*protobuf.ExpandedDeployment
	typeMatcher := protobuf.NewReleaseTypeMatcher(req.TypeFilters)
	if len(req.TypeFilters) == 0 {
		filtered = deployments
	} else {
		filtered = make([]*protobuf.ExpandedDeployment, 0, len(deployments))
		for _, ed := range deployments {
			// filter by type of deployment
			if !typeMatcher.Match(ed.Type) {
				continue
			}
			filtered = append(filtered, ed)
		}
	}

	return filtered, nextPageToken, nil
}

func (s *server) StreamDeployments(req *protobuf.StreamDeploymentsRequest, stream protobuf.Controller_StreamDeploymentsServer) error {
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
		deployments, nextPageToken, err = s.listDeployments(req)
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

	sub, err := s.subscribeEvents(appIDs, []ct.EventType{ct.EventTypeDeployment}, deploymentIDs)
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
				s.logger.Error("error unmarshalling event", "rpcMethod", "StreamDeployments", "error", err)
				continue
			}
			ctd, err := s.deploymentRepo.GetExpanded(event.ObjectID)
			if err != nil {
				s.logger.Error("error fetching deployment for event", "rpcMethod", "StreamDeployments", "deployment_id", event.ObjectID, "error", err)
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

func (s *server) CreateDeployment(req *protobuf.CreateDeploymentRequest, ds protobuf.Controller_CreateDeploymentServer) error {
	appID := protobuf.ParseIDFromName(req.Parent, "apps")
	releaseID := protobuf.ParseIDFromName(req.Parent, "releases")
	d, err := s.deploymentRepo.Add(appID, releaseID)
	if err != nil {
		return protobuf.NewError(err, err.Error())
	}

	// Wait for deployment to complete and perform scale

	sub, err := s.subscribeEvents([]string{appID}, []ct.EventType{ct.EventTypeDeployment}, []string{d.ID})
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
			s.logger.Error("failed to unmarshal deployment event", "event_id", event.ID, "deployment_id", event.ObjectID, "error", err)
			continue
		}

		// Scale release to requested processes/tags once deployment is complete
		if de.Status == "complete" {
			if sr := req.ScaleRequest; sr != nil {
				if _, err := s.createScale(&protobuf.CreateScaleRequest{
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
