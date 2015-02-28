package installer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
)

type assetManifest struct {
	Assets map[string]string `json:"assets"`
}

type htmlTemplateData struct {
	ApplicationJSPath  string
	ApplicationCSSPath string
	ReactJSPath        string
}

type installerJSConfig struct {
	Endpoints            map[string]string `json:"endpoints"`
	HasAWSEnvCredentials bool              `json:"has_aws_env_credentials"`
}

type jsonInput struct {
	Creds        jsonInputCreds `json:"creds"`
	Region       string         `json:"region"`
	InstanceType string         `json:"instance_type"`
	NumInstances int            `json:"num_instances"`
	VpcCidr      string         `json:"vpc_cidr,omitempty"`
	SubnetCidr   string         `json:"subnet_cidr,omitempty"`
}

type jsonInputCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type httpPrompt struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"`
	Message  string `json:"message,omitempty"`
	Yes      bool   `json:"yes,omitempty"`
	Input    string `json:"input,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
	resChan  chan *httpPrompt
	api      *httpAPI
}

type httpEvent struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Prompt      *httpPrompt `json:"prompt,omitempty"`
}

type httpInstaller struct {
	ID            string           `json:"id"`
	Stack         *Stack           `json:"-"`
	PromptOutChan chan *httpPrompt `json:"-"`
	PromptInChan  chan *httpPrompt `json:"-"`
	logger        log.Logger
	subscribeMtx  sync.Mutex
	subscriptions []*httpInstallerSubscription
	eventsMtx     sync.Mutex
	events        []*httpEvent
	err           error
	done          bool
	api           *httpAPI
}

type httpInstallerSubscription struct {
	EventIndex int
	EventChan  chan *httpEvent
	DoneChan   chan struct{}
	ErrChan    chan error
	done       bool
}

func (sub *httpInstallerSubscription) sendEvents(s *httpInstaller) {
	if sub.done {
		return
	}
	for index, event := range s.events {
		if index <= sub.EventIndex {
			continue
		}
		sub.EventIndex = index
		sub.EventChan <- event
	}
}

func (sub *httpInstallerSubscription) handleError(err error) {
	if sub.done {
		return
	}
	sub.ErrChan <- err
}

func (sub *httpInstallerSubscription) handleDone() {
	if sub.done {
		return
	}
	sub.done = true
	close(sub.DoneChan)
}

func (prompt *httpPrompt) Resolve(res *httpPrompt) {
	prompt.api.InstallerPromptsMtx.Lock()
	delete(prompt.api.InstallerPrompts, prompt.ID)
	prompt.api.InstallerPromptsMtx.Unlock()
	prompt.Resolved = true
	prompt.resChan <- res
}

func (s *httpInstaller) YesNoPrompt(msg string) bool {
	prompt := &httpPrompt{
		ID:      random.Hex(16),
		Type:    "yes_no",
		Message: msg,
		resChan: make(chan *httpPrompt),
		api:     s.api,
	}
	prompt.api.InstallerPromptsMtx.Lock()
	prompt.api.InstallerPrompts[prompt.ID] = prompt
	prompt.api.InstallerPromptsMtx.Unlock()

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	res := <-prompt.resChan

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	return res.Yes
}

func (s *httpInstaller) PromptInput(msg string) string {
	prompt := &httpPrompt{
		ID:      random.Hex(16),
		Type:    "input",
		Message: msg,
		resChan: make(chan *httpPrompt),
		api:     s.api,
	}
	s.api.InstallerPromptsMtx.Lock()
	s.api.InstallerPrompts[prompt.ID] = prompt
	s.api.InstallerPromptsMtx.Unlock()

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	res := <-prompt.resChan

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	return res.Input
}

func (s *httpInstaller) Subscribe(eventChan chan *httpEvent) (<-chan struct{}, <-chan error) {
	s.subscribeMtx.Lock()
	defer s.subscribeMtx.Unlock()

	subscription := &httpInstallerSubscription{
		EventIndex: -1,
		EventChan:  eventChan,
		DoneChan:   make(chan struct{}),
		ErrChan:    make(chan error),
	}

	go func() {
		subscription.sendEvents(s)
		if s.err != nil {
			subscription.handleError(s.err)
		}
		if s.done {
			subscription.handleDone()
		}
	}()

	s.subscriptions = append(s.subscriptions, subscription)

	return subscription.DoneChan, subscription.ErrChan
}

func (s *httpInstaller) sendEvent(event *httpEvent) {
	s.eventsMtx.Lock()
	s.events = append(s.events, event)
	s.eventsMtx.Unlock()

	for _, sub := range s.subscriptions {
		go sub.sendEvents(s)
	}
}

func (s *httpInstaller) handleError(err error) {
	for _, sub := range s.subscriptions {
		go sub.handleError(err)
	}
}

func (s *httpInstaller) handleDone() {
	if s.Stack.Domain != nil {
		s.sendEvent(&httpEvent{
			Type:        "domain",
			Description: s.Stack.Domain.Name,
		})
	}
	if s.Stack.DashboardLoginToken != "" {
		s.sendEvent(&httpEvent{
			Type:        "dashboard_login_token",
			Description: s.Stack.DashboardLoginToken,
		})
	}
	if s.Stack.CACert != "" {
		s.sendEvent(&httpEvent{
			Type:        "ca_cert",
			Description: base64.URLEncoding.EncodeToString([]byte(s.Stack.CACert)),
		})
	}
	s.sendEvent(&httpEvent{
		Type: "done",
	})

	for _, sub := range s.subscriptions {
		go sub.handleDone()
	}
}

func (s *httpInstaller) handleEvents() {
	for {
		select {
		case event := <-s.Stack.EventChan:
			s.logger.Info(event.Description)
			s.sendEvent(&httpEvent{
				Type:        "status",
				Description: event.Description,
			})
		case err := <-s.Stack.ErrChan:
			s.logger.Info(err.Error())
			s.handleError(err)
		case <-s.Stack.Done:
			s.handleDone()
			return
		}
	}
	s.logger.Info(s.Stack.DashboardLoginMsg())
}

type httpAPI struct {
	InstallerPrompts    map[string]*httpPrompt
	InstallerPromptsMtx sync.Mutex
	InstallerStacks     map[string]*httpInstaller
	InstallerStackMtx   sync.Mutex
	AWSEnvCreds         aws.CredentialsProvider
}

func ServeHTTP() error {
	api := &httpAPI{
		InstallerPrompts: make(map[string]*httpPrompt),
		InstallerStacks:  make(map[string]*httpInstaller),
	}

	if creds, err := aws.EnvCreds(); err == nil {
		api.AWSEnvCreds = creds
	}

	httpRouter := httprouter.New()

	httpRouter.GET("/", api.ServeTemplate)
	httpRouter.GET("/install", api.ServeTemplate)
	httpRouter.GET("/install/:id", api.ServeTemplate)
	httpRouter.POST("/install", api.InstallHandler)
	httpRouter.GET("/events/:id", api.EventsHandler)
	httpRouter.POST("/prompt/:id", api.PromptHandler)
	httpRouter.GET("/assets/*assetPath", api.ServeAsset)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
	if err := api.OpenAddr(addr); err != nil {
		fmt.Printf("Open %s in your browser to continue.\n", addr)
	}
	return http.Serve(l, api.CorsHandler(httpRouter, addr))
}

func (api *httpAPI) OpenAddr(addr string) error {
	cmds := []string{
		"open",
		"xdg-open",
		"firefox",
		"google-chrome",
	}
	for _, cmdStr := range cmds {
		if _, err := exec.LookPath(cmdStr); err == nil {
			fmt.Printf("Opening %s...\n", addr)
			exec.Command(cmdStr, addr).Start()
			return nil
		}
	}
	return errors.New(fmt.Sprintf("unable to open %s", addr))
}

func (api *httpAPI) CorsHandler(main http.Handler, addr string) http.Handler {
	corsHandler := cors.Allow(&cors.Options{
		AllowOrigins:     []string{addr},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: false,
		MaxAge:           time.Hour,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corsHandler(w, r)
		main.ServeHTTP(w, r)
	})
}

func (api *httpAPI) Asset(path string) (io.ReadSeeker, error) {
	data, err := Asset(path)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (api *httpAPI) AssetManifest() (*assetManifest, error) {
	var manifest *assetManifest
	data, err := api.Asset(filepath.Join("app", "build", "manifest.json"))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(data)
	if err := dec.Decode(&manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (api *httpAPI) InstallHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	var input *jsonInput
	if err := httphelper.DecodeJSON(req, &input); err != nil {
		httphelper.Error(w, err)
		return
	}
	api.InstallerStackMtx.Lock()
	defer api.InstallerStackMtx.Unlock()

	if len(api.InstallerStacks) > 0 {
		httphelper.ObjectExistsError(w, "install already started")
		return
	}

	var id = random.Hex(16)
	var creds aws.CredentialsProvider
	if input.Creds.AccessKeyID != "" && input.Creds.SecretAccessKey != "" {
		creds = aws.Creds(input.Creds.AccessKeyID, input.Creds.SecretAccessKey, "")
	} else {
		var err error
		creds, err = aws.EnvCreds()
		if err != nil {
			httphelper.ValidationError(w, "", err.Error())
			return
		}
	}
	s := &httpInstaller{
		ID:            id,
		PromptOutChan: make(chan *httpPrompt),
		PromptInChan:  make(chan *httpPrompt),
		logger:        log.New(),
		api:           api,
	}
	s.Stack = &Stack{
		Creds:        creds,
		Region:       input.Region,
		InstanceType: input.InstanceType,
		NumInstances: input.NumInstances,
		VpcCidr:      input.VpcCidr,
		SubnetCidr:   input.SubnetCidr,
		PromptInput:  s.PromptInput,
		YesNoPrompt:  s.YesNoPrompt,
	}
	if err := s.Stack.RunAWS(); err != nil {
		httphelper.Error(w, err)
		return
	}
	api.InstallerStacks[id] = s
	go s.handleEvents()
	httphelper.JSON(w, 200, s)
}

func (api *httpAPI) EventsHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	api.InstallerStackMtx.Lock()
	s := api.InstallerStacks[params.ByName("id")]
	api.InstallerStackMtx.Unlock()
	if s == nil {
		httphelper.ObjectNotFoundError(w, "install instance not found")
		return
	}

	eventChan := make(chan *httpEvent)
	doneChan, errChan := s.Subscribe(eventChan)

	stream := sse.NewStream(w, eventChan, s.logger)
	stream.Serve()

	s.logger.Info(fmt.Sprintf("streaming events for %s", s.ID))

	go func() {
		for {
			select {
			case err := <-errChan:
				s.logger.Info(err.Error())
				stream.Error(err)
			case <-doneChan:
				stream.Close()
				return
			}
		}
	}()

	stream.Wait()
}

func (api *httpAPI) PromptHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	api.InstallerPromptsMtx.Lock()
	prompt := api.InstallerPrompts[params.ByName("id")]
	api.InstallerPromptsMtx.Unlock()
	if prompt == nil {
		httphelper.ObjectNotFoundError(w, "prompt not found")
		return
	}

	var input *httpPrompt
	if err := httphelper.DecodeJSON(req, &input); err != nil {
		httphelper.Error(w, err)
		return
	}
	prompt.Resolve(input)
	w.WriteHeader(200)
}

func (api *httpAPI) ServeApplicationJS(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	path := filepath.Join("app", "build", params.ByName("assetPath"))
	data, err := api.Asset(path)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(500)
		return
	}

	var jsConf bytes.Buffer
	jsConf.Write([]byte("window.InstallerConfig = "))
	json.NewEncoder(&jsConf).Encode(installerJSConfig{
		Endpoints: map[string]string{
			"install": "/install",
			"events":  "/events/:id",
			"prompt":  "/prompt/:id",
		},
		HasAWSEnvCredentials: api.AWSEnvCreds != nil,
	})
	jsConf.Write([]byte(";\n"))

	r := ioutil.NewMultiReadSeeker(bytes.NewReader(jsConf.Bytes()), data)

	http.ServeContent(w, req, path, time.Now(), r)
}

func (api *httpAPI) ServeAsset(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if strings.HasPrefix(params.ByName("assetPath"), "/application-") {
		api.ServeApplicationJS(w, req, params)
	} else {
		path := filepath.Join("app", "build", params.ByName("assetPath"))
		data, err := api.Asset(path)
		if err != nil {
			httphelper.Error(w, err)
			return
		}
		http.ServeContent(w, req, path, time.Now(), data)
	}
}

func (api *httpAPI) ServeTemplate(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if req.Header.Get("Accept") == "application/json" {
		api.InstallerStackMtx.Lock()
		s := api.InstallerStacks[params.ByName("id")]
		if s == nil && len(api.InstallerStacks) > 0 {
			for id := range api.InstallerStacks {
				s = api.InstallerStacks[id]
				break
			}
		}
		api.InstallerStackMtx.Unlock()
		if s == nil {
			w.WriteHeader(404)
			return
		}
		httphelper.JSON(w, 200, s)
		return
	}

	manifest, err := api.AssetManifest()
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(500)
		return
	}

	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Cache-Control", "max-age=0")

	err = htmlTemplate.Execute(w, &htmlTemplateData{
		ApplicationJSPath:  manifest.Assets["application.js"],
		ApplicationCSSPath: manifest.Assets["application.css"],
		ReactJSPath:        manifest.Assets["react.js"],
	})
	if err != nil {
		w.WriteHeader(500)
		fmt.Println(err)
		return
	}
}
