package installer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"gopkg.in/inconshreveable/log15.v2"

	"github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/installer"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/browser"
	"golang.org/x/net/context"
)

var logger = log15.New()

// ServeHTTP starts a server for the web interface
// and attempts to open it in the default browser
func ServeHTTP(port string) error {
	if port == "" {
		port = "0"
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", port))
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
	fmt.Printf("Open %s in your browser to continue.\n", addr)
	browser.OpenURL(addr)

	return http.Serve(l, corsHandler(apiHandler(), addr))
}

func corsHandler(main http.Handler, addr string) http.Handler {
	return (&cors.Options{
		AllowOrigins:     []string{addr},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: false,
		MaxAge:           time.Hour,
	}).Handler(main)
}

type installerJSConfig struct {
	Endpoints            map[string]string `json:"endpoints"`
	HasAWSEnvCredentials bool              `json:"has_aws_env_credentials"`
	AWSEnvCredentialsID  string            `json:"aws_env_credentials_id,omitempty"`
}

type assetManifest struct {
	Assets map[string]string `json:"assets"`
}

type htmlTemplateData struct {
	ApplicationJSPath  string
	NormalizeCSSPath   string
	FontAwesomeCSSPath string
	ApplicationCSSPath string
	ReactJSPath        string
}

type httpAPI struct {
	jsAppConfig           installerJSConfig
	events                chan *installer.Event
	eventSubscriptions    []chan *installer.Event
	eventSubscriptionsMux sync.Mutex
	pendingPrompts        map[string]installer.Prompt
	pendingPromptsMux     sync.Mutex
}

var api *httpAPI

func apiHandler() http.Handler {
	api = &httpAPI{
		events:         make(chan *installer.Event),
		pendingPrompts: make(map[string]installer.Prompt),
		jsAppConfig: installerJSConfig{
			Endpoints: map[string]string{
				"launch_cluster":      "/clusters",
				"destroy_cluster":     "/clusters/:id",
				"upload_backup":       "/clusters/:id/backup",
				"respond_to_prompt":   "/prompts/:id",
				"events":              "/events",
				"add_credential":      "/credentials",
				"remove_credential":   "/credentials/:id",
				"regions":             "/regions/:cloud",
				"azure_subscriptions": "/azure/subscriptions",
			},
		},
	}

	// Handle event subscriptions
	go func() {
		for {
			if e, ok := <-api.events; ok {
				api.sendEvent(e)
			} else {
				return
			}
		}
	}()

	r := httprouter.New()
	r2 := httprouter.New()
	r.NotFound = r2.ServeHTTP
	r2.GET("/*path", httphelper.WrapHandler(api.ServeTemplate))
	r.GET("/assets/*assetPath", httphelper.WrapHandler(api.ServeAsset))

	r.POST("/clusters", httphelper.WrapHandler(api.LaunchCluster))
	r.DELETE("/clusters/:id", httphelper.WrapHandler(api.DestroyCluster))
	r.POST("/clusters/:id/backup", httphelper.WrapHandler(api.ReceiveBackup))
	r.POST("/prompts/:id", httphelper.WrapHandler(api.ReceivePromptResponse))
	r.GET("/events", httphelper.WrapHandler(api.StreamEvents))
	r.POST("/credentials", httphelper.WrapHandler(api.AddCredential))
	r.DELETE("/credentials/:type/:id", httphelper.WrapHandler(api.RemoveCredential))
	r.GET("/regions/:cloud", httphelper.WrapHandler(api.GetCloudRegions))
	r.GET("/azure/subscriptions", httphelper.WrapHandler(api.GetAzureSubscriptions))

	return httphelper.ContextInjector("installer", r)
}

func (api *httpAPI) watchEvents(ch <-chan *installer.Event) {
	for {
		if e, ok := <-ch; ok {
			api.events <- e
		} else {
			return
		}
	}
}

func (api *httpAPI) sendEvent(event *installer.Event) {
	api.eventSubscriptionsMux.Lock()
	defer api.eventSubscriptionsMux.Unlock()
	if event.Type == installer.EventTypePrompt {
		api.addPendingPrompt(event.Payload.(installer.Prompt))
	}
	for _, ch := range api.eventSubscriptions {
		ch := ch
		go func() {
			ch <- event
		}()
	}
}

func (api *httpAPI) addPendingPrompt(prompt installer.Prompt) {
	api.pendingPromptsMux.Lock()
	defer api.pendingPromptsMux.Unlock()
	api.pendingPrompts[prompt.ID()] = prompt
}

func (api *httpAPI) respondToPrompt(id string, res io.Reader) error {
	api.pendingPromptsMux.Lock()
	defer api.pendingPromptsMux.Unlock()
	if prompt, ok := api.pendingPrompts[id]; ok {
		delete(api.pendingPrompts, id)
		example := prompt.ResponseExample()
		if example == nil {
			prompt.Respond(res)
		} else {
			data := reflect.New(reflect.TypeOf(example)).Interface()
			if err := json.NewDecoder(res).Decode(&data); err != nil {
				return err
			}
			prompt.Respond(data)
		}
	} else {
		return fmt.Errorf("prompt with id %q not found", id)
	}
	return nil
}

func (api *httpAPI) Asset(path string) (io.ReadSeeker, error) {
	data, err := Asset(path)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (api *httpAPI) AssetManifest() (*assetManifest, error) {
	data, err := api.Asset(filepath.Join("app", "build", "manifest.json"))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(data)
	var manifest *assetManifest
	if err := dec.Decode(&manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (api *httpAPI) ServeApplicationJS(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	path := filepath.Join("app", "build", params.ByName("assetPath"))
	data, err := api.Asset(path)
	if err != nil {
		httphelper.Error(w, err)
		logger.Debug(err.Error())
		return
	}

	var jsConf bytes.Buffer
	jsConf.Write([]byte("window.InstallerConfig = "))
	json.NewEncoder(&jsConf).Encode(api.jsAppConfig)
	jsConf.Write([]byte(";\n"))

	r := ioutil.NewMultiReadSeeker(bytes.NewReader(jsConf.Bytes()), data)

	http.ServeContent(w, req, path, time.Now(), r)
}

func (api *httpAPI) ServeAsset(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	if strings.HasPrefix(params.ByName("assetPath"), "/application-") && strings.HasSuffix(params.ByName("assetPath"), ".js") {
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

func (api *httpAPI) ServeTemplate(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	manifest, err := api.AssetManifest()
	if err != nil {
		httphelper.Error(w, err)
		logger.Debug(err.Error())
		return
	}

	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Cache-Control", "max-age=0")

	err = htmlTemplate.Execute(w, &htmlTemplateData{
		ApplicationJSPath:  manifest.Assets["application.js"],
		NormalizeCSSPath:   manifest.Assets["normalize.css"],
		FontAwesomeCSSPath: manifest.Assets["font-awesome.css"],
		ApplicationCSSPath: manifest.Assets["application.css"],
		ReactJSPath:        manifest.Assets["react.js"],
	})
	if err != nil {
		httphelper.Error(w, err)
		logger.Debug(err.Error())
		return
	}
}

func (api *httpAPI) LaunchCluster(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var data bytes.Buffer
	if _, err := io.Copy(&data, req.Body); err != nil {
		httphelper.Error(w, err)
		return
	}
	cluster, err := installer.UnmarshalCluster(data.Bytes())
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	go api.watchEvents(installer.LaunchCluster(cluster))
}

func (api *httpAPI) DestroyCluster(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) ReceiveBackup(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) ReceivePromptResponse(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	params, _ := ctxhelper.ParamsFromContext(ctx)
	if err := api.respondToPrompt(params.ByName("id"), req.Body); err != nil {
		httphelper.ObjectNotFoundError(w, err.Error())
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) StreamEvents(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) AddCredential(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) RemoveCredential(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) GetCloudRegions(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}

func (api *httpAPI) GetAzureSubscriptions(ctx context.Context, w http.ResponseWriter, req *http.Request) {
}
