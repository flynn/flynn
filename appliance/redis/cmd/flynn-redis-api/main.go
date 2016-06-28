package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	// DefaultServiceName is the service name used if FLYNN_REDIS is empty.
	DefaultServiceName = "redis"

	// DefaultAddr is the default bind address for the HTTP API.
	DefaultAddr = ":3000"

	// PasswordLength is the size of generated Redis passwords.
	// Redis can test passwords quickly so this provides 16^20 (1.2e24) possible combinations.
	PasswordLength = 20
)

func main() {
	m := NewMain()
	if err := m.ParseFlags(os.Args[1:]); err != nil {
		shutdown.Fatal(err)
	}

	if err := m.Run(); err != nil {
		shutdown.Fatal(err)
	}
	<-(chan struct{})(nil)
}

// Main represent the main program.
type Main struct {
	ln net.Listener

	// Name of the service to register with discoverd.
	ServiceName string

	// Bind address & handler for the HTTP API.
	Addr    string
	Handler *Handler

	Logger log15.Logger

	// Standard input/output
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewMain returns a new instance of Main.
func NewMain() *Main {
	return &Main{
		ServiceName: DefaultServiceName,

		Addr:    DefaultAddr,
		Handler: NewHandler(),

		Logger: log15.New("app", "redis-api"),

		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Listener returns the program's listener.
func (m *Main) Listener() net.Listener { return m.ln }

// ServiceHost returns the discoverd leader host.
func (m *Main) ServiceHost() string {
	return fmt.Sprintf("leader.%s.discoverd", m.ServiceName)
}

// ParseFlags parses the command line flags and environment variables.
func (m *Main) ParseFlags(args []string) error {
	if s := os.Getenv("FLYNN_REDIS"); s != "" {
		m.ServiceName = s
	}

	if port := os.Getenv("PORT"); port != "" {
		m.Addr = ":" + port
	}
	m.Handler.RedisImageID = os.Getenv("REDIS_IMAGE_ID")

	// Connect to controller.
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		m.Logger.Error("cannot connect to controller", "err", err)
		return err
	}
	m.Handler.ServiceName = m.ServiceName
	m.Handler.ControllerClient = client

	return nil
}

// Run executes the program.
func (m *Main) Run() error {
	// Open HTTP port.
	ln, err := net.Listen("tcp", m.Addr)
	if err != nil {
		return err
	}
	m.ln = ln

	// Initialize handler logger.
	m.Handler.Logger = m.Logger.New("component", "http")

	// Register service with discoverd.
	hb, err := discoverd.AddServiceAndRegister(m.ServiceName+"-api", m.Addr)
	if err != nil {
		return err
	}
	shutdown.BeforeExit(func() { hb.Close() })

	h := httphelper.ContextInjector(m.ServiceName+"-api", httphelper.NewRequestLogger(m.Handler))
	go func() { http.Serve(ln, h) }()

	return nil
}

// Close cleanly shuts down the program.
func (m *Main) Close() error {
	logger := m.Logger.New("fn", "Close")

	if m.ln != nil {
		if err := m.ln.Close(); err != nil {
			logger.Error("error closing listener", "err", err)
		}
	}

	return nil
}

// Handler represents an HTTP handler for the provisioning API.
type Handler struct {
	router *httprouter.Router

	// Name of the running service.
	ServiceName string

	// Key used to access the controller.
	ControllerClient controller.Client

	RedisImageID string

	Logger log15.Logger
}

// NewAPIHandler returns a new instance of APIHandler.
func NewHandler() *Handler {
	h := &Handler{
		router: httprouter.New(),
		Logger: log15.New(),
	}
	h.router.POST("/clusters", h.servePostCluster)
	h.router.DELETE("/clusters", h.serveDeleteCluster)
	h.router.GET("/ping", h.serveGetPing)
	return h
}

// ServeHTTP serves an HTTP request and returns a response.
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) { h.router.ServeHTTP(w, req) }

func (h *Handler) servePostCluster(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Generate a password for the redis instance to use.
	password := random.String(PasswordLength)

	// Generate a new service name for each deployment of Redis.
	serviceName := "redis-" + random.UUID()

	release := &ct.Release{
		ArtifactIDs: []string{h.RedisImageID},
		Meta:        make(map[string]string),
		Processes: map[string]ct.ProcessType{
			"redis": {
				Ports: []ct.Port{
					{Port: 6379, Proto: "tcp"},
					{Port: 6380, Proto: "tcp"},
				},
				Volumes: []ct.VolumeReq{{Path: "/data"}},
				Args:    []string{"/bin/start-flynn-redis", "redis"},
				Service: serviceName,
			},
		},
		Env: map[string]string{
			"FLYNN_REDIS":    serviceName,
			"REDIS_PASSWORD": password,
		},
	}

	// Create an app for this redis cluster.
	app := &ct.App{
		Name: serviceName,
		Meta: map[string]string{"flynn-system-app": "true"},
	}
	if err := h.ControllerClient.CreateApp(app); err != nil {
		h.Logger.Error("error creating app", "err", err)
		httphelper.Error(w, err)
		return
	}

	h.Logger.Info("creating release", "artifact.id", h.RedisImageID)
	if err := h.ControllerClient.CreateRelease(app.ID, release); err != nil {
		h.Logger.Error("error creating release", "err", err)
		httphelper.Error(w, err)
		return
	}

	h.Logger.Info("put formation", "release.id", release.ID)
	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"redis": 1},
	}
	if err := h.ControllerClient.PutFormation(formation); err != nil {
		h.Logger.Error("error deploying release", "err", err)
		httphelper.Error(w, err)
		return
	}
	h.Logger.Info("formation", "formation", fmt.Sprintf("%#v", formation))

	h.Logger.Info("deploying app release", "release.ID", release.ID)
	timeoutCh := make(chan struct{})
	time.AfterFunc(5*time.Minute, func() { close(timeoutCh) })
	if err := h.ControllerClient.DeployAppRelease(app.ID, release.ID, timeoutCh); err != nil {
		h.Logger.Error("error deploying release", "err", err)
		httphelper.Error(w, err)
		return
	}

	host := fmt.Sprintf("leader.%s.discoverd", app.Name)
	u := url.URL{
		Scheme: "redis",
		Host:   host + ":6379",
		User:   url.UserPassword("", password),
	}

	httphelper.JSON(w, 200, resource.Resource{
		ID: fmt.Sprintf("/clusters/%s", release.ID),
		Env: map[string]string{
			"FLYNN_REDIS":    app.Name,
			"REDIS_HOST":     host,
			"REDIS_PORT":     "6379",
			"REDIS_PASSWORD": password,
			"REDIS_URL":      u.String(),
		},
	})
}

func (h *Handler) serveDeleteCluster(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Extract release ID.
	h.Logger.Info("parsing id", "id", req.FormValue("id"))
	releaseID := strings.TrimPrefix(req.FormValue("id"), "/clusters/")
	if releaseID == "" {
		h.Logger.Error("error parsing id", "id", req.FormValue("id"))
		httphelper.ValidationError(w, "id", "is invalid")
		return
	}

	// Retrieve release.
	h.Logger.Info("retrieving release", "release.id", releaseID)
	release, err := h.ControllerClient.GetRelease(releaseID)
	if err != nil {
		h.Logger.Error("error finding release", "err", err, "release.id", releaseID)
		httphelper.Error(w, err)
		return
	}

	// Retrieve app name from env variable.
	appName := release.Env["FLYNN_REDIS"]
	if appName == "" {
		h.Logger.Error("unable to find app name", "release.id", releaseID)
		httphelper.Error(w, errors.New("unable to find app name"))
		return
	}
	h.Logger.Info("found release app", "app.name", appName)

	// Destroy app release.
	h.Logger.Info("destroying app", "app.name", appName)
	if _, err := h.ControllerClient.DeleteApp(appName); err != nil {
		h.Logger.Error("error destroying app", "err", err)
		httphelper.Error(w, err)
		return
	}

	w.WriteHeader(200)
}

func (h *Handler) serveGetPing(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	w.WriteHeader(200)
}
