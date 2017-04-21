package installer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/pkg/azure"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/util/release/types"
	"github.com/flynn/oauth2"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/browser"
	log "gopkg.in/inconshreveable/log15.v2"
)

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

type installerJSConfig struct {
	ReleaseChannels      []*ReleaseChannel     `json:"release_channels"`
	EC2Versions          []*release.EC2Version `json:"ec2_versions"`
	Endpoints            map[string]string     `json:"endpoints"`
	HasAWSEnvCredentials bool                  `json:"has_aws_env_credentials"`
	AWSEnvCredentialsID  string                `json:"aws_env_credentials_id,omitempty"`
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
			ReleaseChannels: installer.releaseChannels,
			EC2Versions:     installer.ec2Versions,
			Endpoints: map[string]string{
				"clusters":           "/clusters",
				"cluster":            "/clusters/:id",
				"upload_backup":      "/clusters/:id/upload-backup",
				"cert":               "/clusters/:id/ca-cert",
				"events":             "/events",
				"prompt":             "/clusters/:id/prompts/:prompt_id",
				"credentials":        "/credentials",
				"regions":            "/regions",
				"azureSubscriptions": "/azure/subscriptions",
			},
		},
	}

	if creds, err := aws.EnvCreds(); err == nil {
		api.AWSEnvCreds = creds
		if c, err := creds.Credentials(); err == nil {
			api.clientConfig.HasAWSEnvCredentials = true
			api.clientConfig.AWSEnvCredentialsID = c.AccessKeyID
		}
	}

	httpRouter := httprouter.New()

	httpRouter.GET("/", api.ServeTemplate)
	httpRouter.GET("/credentials", api.ServeTemplate)
	httpRouter.GET("/credentials/:id", api.ServeTemplate)
	httpRouter.GET("/clusters/:id", api.ServeTemplate)
	httpRouter.POST("/clusters/:id/upload-backup", api.ReceiveBackup)
	httpRouter.GET("/clusters/:id/ca-cert", api.GetCert)
	httpRouter.GET("/clusters/:id/delete", api.ServeTemplate)
	httpRouter.GET("/oauth/azure", api.ServeTemplate)
	httpRouter.DELETE("/clusters/:id", api.DeleteCluster)
	httpRouter.GET("/clusters", api.RedirectRoot)
	httpRouter.POST("/clusters", api.LaunchCluster)
	httpRouter.GET("/events", api.Events)
	httpRouter.POST("/clusters/:id/prompts/:prompt_id", api.Prompt)
	httpRouter.GET("/assets/*assetPath", api.ServeAsset)
	httpRouter.POST("/credentials", api.NewCredential)
	httpRouter.DELETE("/credentials/:type/:id", api.DeleteCredential)
	httpRouter.GET("/regions", api.GetCloudRegions)
	httpRouter.GET("/azure/subscriptions", api.GetAzureSubscriptions)

	port := os.Getenv("PORT")
	if port == "" {
		// if no port is given, use a random one
		port = "0"
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
	fmt.Printf("Open %s in your browser to continue.\n", addr)
	browser.OpenURL(addr)
	return http.Serve(l, api.CorsHandler(httpRouter, addr))
}

func (api *httpAPI) CorsHandler(main http.Handler, addr string) http.Handler {
	return (&cors.Options{
		AllowOrigins:     []string{addr},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: false,
		MaxAge:           time.Hour,
	}).Handler(main)
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

func (api *httpAPI) RedirectRoot(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	http.Redirect(w, req, "/", 302)
}

func (api *httpAPI) LaunchCluster(w http.ResponseWriter, req *http.Request, params httprouter.Params) {

	var inputJSON bytes.Buffer
	if _, err := inputJSON.ReadFrom(req.Body); err != nil {
		httphelper.Error(w, err)
		return
	}

	decodeJSON := func(dst interface{}) error {
		return json.Unmarshal(inputJSON.Bytes(), dst)
	}

	var base *BaseCluster
	if err := decodeJSON(&base); err != nil {
		httphelper.Error(w, err)
		return
	}

	if base.CredentialID == "" && base.Type != "ssh" {
		httphelper.ValidationError(w, "credential_id", "Missing credential id")
		return
	}

	var creds *Credential
	if base.Type == "aws" && base.CredentialID == "aws_env" {
		creds = &Credential{
			ID: base.CredentialID,
		}
	} else if base.Type != "ssh" {
		var err error
		creds, err = api.Installer.FindCredentials(base.CredentialID)
		if err != nil {
			httphelper.ValidationError(w, "credential_id", "Invalid credential id")
			return
		}
	}

	var cluster Cluster
	switch base.Type {
	case "aws":
		cluster = &AWSCluster{}
	case "digital_ocean":
		cluster = &DigitalOceanCluster{}
	case "azure":
		cluster = &AzureCluster{}
	case "ssh":
		cluster = &SSHCluster{}
	default:
		httphelper.ValidationError(w, "type", fmt.Sprintf("Invalid type \"%s\"", base.Type))
		return
	}

	base.ID = fmt.Sprintf("flynn-%d", time.Now().Unix())
	base.State = "starting"
	base.installer = api.Installer

	if err := decodeJSON(&cluster); err != nil {
		httphelper.Error(w, err)
		return
	}

	cluster.SetBase(base)

	if err := cluster.SetCreds(creds); err != nil {
		httphelper.Error(w, err)
		return
	}

	if err := api.Installer.LaunchCluster(cluster); err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, base)
}

func (api *httpAPI) ReceiveBackup(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	cluster, err := api.Installer.FindBaseCluster(params.ByName("id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, err.Error())
		return
	}
	defer req.Body.Close()
	size, err := strconv.Atoi(req.Header.Get("Content-Length"))
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	if err := cluster.ReceiveBackup(req.Body, size); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) GetCert(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	cluster, err := api.Installer.FindBaseCluster(params.ByName("id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="flynn-ca.cer"`)
	w.Write([]byte(cluster.CACert))
}

func (api *httpAPI) DeleteCluster(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if err := api.Installer.DeleteCluster(params.ByName("id")); err != nil {
		if err == ClusterNotFoundError {
			httphelper.ObjectNotFoundError(w, err.Error())
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
	sub := api.Installer.Subscribe(eventChan, lastEventID)
	defer api.Installer.Unsubscribe(sub)
	sse.ServeStream(w, eventChan, api.logger)
}

func (api *httpAPI) Prompt(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	cluster, err := api.Installer.FindCluster(params.ByName("id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, "cluster not found")
		return
	}
	prompt, err := cluster.Base().findPrompt(params.ByName("prompt_id"))
	if err != nil {
		httphelper.ObjectNotFoundError(w, "prompt not found")
		return
	}

	defer req.Body.Close()
	var input *Prompt
	if prompt.Type == PromptTypeFile {
		size, err := strconv.Atoi(req.Header.Get("Content-Length"))
		if err != nil {
			httphelper.Error(w, err)
			return
		}
		input = &Prompt{
			File:     req.Body,
			FileSize: size,
		}
	} else {
		if err := httphelper.DecodeJSON(req, &input); err != nil {
			httphelper.Error(w, err)
			return
		}
	}
	if err := prompt.Resolve(input); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) NewCredential(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	creds := &Credential{}
	if err := httphelper.DecodeJSON(req, &creds); err != nil {
		httphelper.Error(w, err)
		return
	}
	if creds.Type == "azure" {
		oauthCreds := make([]*OAuthCredential, 0, 2)
		for _, resource := range []string{azure.JSONAPIResource, azure.XMLAPIResource} {
			token, err := azure.OAuth2Config(creds.ID, creds.Endpoint, resource).Exchange(oauth2.NoContext, creds.Secret)
			if err != nil {
				httphelper.Error(w, err)
				return
			}
			oauthCreds = append(oauthCreds, &OAuthCredential{
				ClientID:     creds.ID,
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				ExpiresAt:    &token.Expiry,
				Scope:        resource,
			})
		}
		creds.Secret = ""
		creds.OAuthCreds = oauthCreds
	}
	if err := api.Installer.SaveCredentials(creds); err != nil {
		if err == credentialExistsError {
			httphelper.ObjectExistsError(w, err.Error())
			return
		}
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) DeleteCredential(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if err := api.Installer.DeleteCredentials(params.ByName("id")); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (api *httpAPI) GetCloudRegions(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	params := req.URL.Query()
	cloud := params.Get("cloud")
	if cloud != "digital_ocean" && cloud != "azure" {
		httphelper.ObjectNotFoundError(w, "")
		return
	}
	credentialID := params.Get("credential_id")
	creds, err := api.Installer.FindCredentials(credentialID)
	if err != nil {
		httphelper.ValidationError(w, "credential_id", "Invalid credential id")
		return
	}
	var res interface{}
	switch cloud {
	case "digital_ocean":
		res, err = api.Installer.ListDigitalOceanRegions(creds)
	case "azure":
		res, err = api.Installer.ListAzureRegions(creds)
	}
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (api *httpAPI) GetAzureSubscriptions(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	params := req.URL.Query()
	credentialID := params.Get("credential_id")
	creds, err := api.Installer.FindCredentials(credentialID)
	if err != nil {
		httphelper.ValidationError(w, "credential_id", "Invalid credential id")
		return
	}
	client := api.Installer.azureClient(creds)
	res, err := client.ListSubscriptions()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (api *httpAPI) ServeApplicationJS(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	path := filepath.Join("app", "build", params.ByName("assetPath"))
	data, err := api.Asset(path)
	if err != nil {
		httphelper.Error(w, err)
		api.logger.Debug(err.Error())
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
		s, err := api.Installer.FindBaseCluster(params.ByName("id"))
		if err != nil {
			httphelper.ObjectNotFoundError(w, err.Error())
			return
		}
		httphelper.JSON(w, 200, s)
		return
	}

	manifest, err := api.AssetManifest()
	if err != nil {
		httphelper.Error(w, err)
		api.logger.Debug(err.Error())
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
		api.logger.Debug(err.Error())
		return
	}
}
