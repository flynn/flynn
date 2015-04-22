package installer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/pkg/browser"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/httphelper"
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
	NumInstances int64          `json:"num_instances"`
	VpcCidr      string         `json:"vpc_cidr,omitempty"`
	SubnetCidr   string         `json:"subnet_cidr,omitempty"`
}

type jsonInputCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type httpAPI struct {
	AWSEnvCreds  aws.CredentialsProvider
	Installer    *Installer
	logger       log.Logger
	clientConfig installerJSConfig
}

func ServeHTTP() error {
	logger := log.New()
	installer := NewInstaller(logger)

	api := &httpAPI{
		Installer: installer,
		logger:    logger,
		clientConfig: installerJSConfig{
			Endpoints: map[string]string{
				"clusters": "/clusters",
				"cluster":  "/clusters/:id",
				"events":   "/events",
				"prompt":   "/clusters/:id/prompts/:prompt_id",
			},
		},
	}

	if creds, err := aws.EnvCreds(); err == nil {
		api.AWSEnvCreds = creds
	}
	api.clientConfig.HasAWSEnvCredentials = api.AWSEnvCreds != nil

	httpRouter := httprouter.New()

	httpRouter.GET("/", api.ServeTemplate)
	httpRouter.GET("/clusters/:id", api.ServeTemplate)
	httpRouter.GET("/clusters/:id/delete", api.ServeTemplate)
	httpRouter.DELETE("/clusters/:id", api.DeleteCluster)
	httpRouter.POST("/clusters", api.LaunchCluster)
	httpRouter.GET("/events", api.Events)
	httpRouter.POST("/clusters/:id/prompts/:prompt_id", api.Prompt)
	httpRouter.GET("/assets/*assetPath", api.ServeAsset)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
	fmt.Printf("Open %s in your browser to continue.\n", addr)
	browser.OpenURL(addr)
	return http.Serve(l, api.CorsHandler(httpRouter, addr))
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

func (api *httpAPI) LaunchCluster(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	var input *jsonInput
	if err := httphelper.DecodeJSON(req, &input); err != nil {
		httphelper.Error(w, err)
		return
	}

	var creds aws.CredentialsProvider
	var credentialID string
	var credentialSecret string
	if input.Creds.AccessKeyID != "" && input.Creds.SecretAccessKey != "" {
		creds = aws.Creds(input.Creds.AccessKeyID, input.Creds.SecretAccessKey, "")
		credentialID = input.Creds.AccessKeyID
		credentialSecret = input.Creds.SecretAccessKey
	} else {
		var err error
		creds, err = aws.EnvCreds()
		if err != nil {
			httphelper.ValidationError(w, "", err.Error())
			return
		}
		credentialID = "aws_env"
	}
	api.Installer.SaveAWSCredentials(credentialID, credentialSecret)
	c := &AWSCluster{
		StackName:    fmt.Sprintf("flynn-%d", time.Now().Unix()),
		Region:       input.Region,
		InstanceType: input.InstanceType,
		VpcCidr:      input.VpcCidr,
		SubnetCidr:   input.SubnetCidr,
		creds:        creds,
	}
	c.cluster = &Cluster{
		ID:           c.StackName,
		State:        "starting",
		CredentialID: credentialID,
		NumInstances: input.NumInstances,
		installer:    api.Installer,
	}
	if err := api.Installer.LaunchCluster(c); err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, c.cluster)
}

func (api *httpAPI) DeleteCluster(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if err := api.Installer.DeleteCluster(params.ByName("id")); err != nil {
		if err == ClusterNotFoundError {
			w.WriteHeader(404)
			return
		}
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) Events(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	eventChan := make(chan *Event)
	lastEventID := req.Header.Get("Last-Event-ID")
	api.Installer.Subscribe(eventChan, lastEventID)

	stream := sse.NewStream(w, eventChan, api.logger)
	stream.Serve()

	api.logger.Debug("streaming events")

	stream.Wait()
}

func (api *httpAPI) Prompt(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	s, err := api.Installer.FindCluster(params.ByName("id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, "cluster not found")
		return
	}
	prompt, err := s.findPrompt(params.ByName("prompt_id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, "prompt not found")
		return
	}

	var input *Prompt
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
	json.NewEncoder(&jsConf).Encode(api.clientConfig)
	jsConf.Write([]byte(";\n"))

	r := ioutil.NewMultiReadSeeker(bytes.NewReader(jsConf.Bytes()), data)

	http.ServeContent(w, req, path, time.Now(), r)
}

func (api *httpAPI) ServeAsset(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
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

func (api *httpAPI) ServeTemplate(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if req.Header.Get("Accept") == "application/json" {
		s, err := api.Installer.FindCluster(params.ByName("id"))
		if err != nil {
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
