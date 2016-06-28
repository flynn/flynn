package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	cc "github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/client/v1"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	logagg "github.com/flynn/flynn/logaggregator/types"
	g "github.com/flynn/flynn/pkg/examplegenerator"
	"github.com/flynn/flynn/pkg/httprecorder"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/typeconv"
	"github.com/flynn/flynn/router/types"
)

type generator struct {
	conf        *config
	client      cc.Client
	recorder    *httprecorder.Recorder
	resourceIds map[string]string
}

func main() {
	conf, err := loadConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(conf.logOut)

	httpClient := &http.Client{}
	client, err := cc.NewClientWithHTTP("", conf.controllerKey, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	recorder := httprecorder.NewWithClient(httpClient)

	e := &generator{
		conf:        conf,
		client:      client,
		recorder:    recorder,
		resourceIds: make(map[string]string),
	}

	providerLog := log.New(conf.logOut, "provider: ", 1)
	go e.listenAndServe(providerLog)

	examples := []g.Example{
		// Run provider_create first so that discoverd service has time to
		// propagate
		{"provider_create", e.createProvider},
		{"app_create_error", e.createAppError},
		{"app_create", e.createApp},
		{"app_initial_release_get", e.getInitialAppRelease},
		{"app_get", e.getApp},
		{"app_list", e.listApps},
		{"app_log", e.getAppLog},
		{"app_log_stream", e.streamAppLog},
		{"app_update", e.updateApp},
		{"route_create", e.createRoute},
		{"route_get", e.getRoute},
		{"route_update", e.updateRoute},
		{"route_list", e.listRoutes},
		{"route_delete", e.deleteRoute},
		{"artifact_create", e.createArtifact},
		{"release_create", e.createRelease},
		{"artifact_list", e.listArtifacts},
		{"release_list", e.listReleases},
		{"app_release_set", e.setAppRelease},
		{"app_release_get", e.getAppRelease},
		{"app_release_list", e.listAppReleases},
		{"formation_put", e.putFormation},
		{"formation_get", e.getFormation},
		{"formation_get_expanded", e.getExpandedFormation},
		{"formation_list", e.listFormations},
		{"formations_list_active", e.listActiveFormations},
		{"formations_stream", e.streamFormations},
		{"release_create2", e.createRelease},
		{"deployment_create", e.createDeployment},
		{"deployment_get", e.getDeployment},
		{"deployment_list", e.listDeployments},
		{"formation_delete", e.deleteFormation},
		{"job_run", e.runJob},
		{"job_list", e.listJobs},
		{"job_update", e.updateJob},
		{"job_get", e.getJob},
		{"job_delete", e.deleteJob},
		{"provider_get", e.getProvider},
		{"provider_list", e.listProviders},
		{"provider_resource_create", e.createProviderResource},
		{"provider_resource_put", e.putProviderResource},
		{"app_resource_list", e.listAppResources},
		{"provider_resource_get", e.getProviderResource},
		{"provider_resource_list", e.listProviderResources},
		{"provider_resource_delete", e.deleteProviderResource},
		{"app_delete", e.deleteApp},
		{"events_list", e.eventsList},
		{"events_stream", e.eventsStream},
		{"event_get", e.eventGet},
		{"ca_cert", e.getCACert},
		{"cluster_backup", e.clusterBackup},
	}

	if os.Getenv("SKIP_MIGRATE_DOMAIN") != "true" {
		examples = append(examples, g.Example{"migrate_cluster_domain", e.migrateClusterDomain})
	}

	var out io.Writer
	if len(os.Args) > 1 {
		var err error
		out, err = os.Create(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
	} else {
		out = os.Stdout
	}
	if err := g.WriteOutput(recorder, examples, out); err != nil {
		log.Fatal(err)
	}
}

func (e *generator) listenAndServe(l *log.Logger) {
	l.Printf("Starting mock provider server on port %s\n", e.conf.ourPort)
	http.HandleFunc("/providers/", func(w http.ResponseWriter, r *http.Request) {
		l.Printf("%s %s\n", r.Method, r.URL)
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		body := buf.String()
		l.Printf("\t%s\n", body)

		resource := &resource.Resource{
			Env: map[string]string{
				"some": "data",
			},
		}
		err := json.NewEncoder(w).Encode(resource)
		if err != nil {
			l.Println(err)
			w.WriteHeader(500)
			return
		}
	})

	http.ListenAndServe(":"+e.conf.ourPort, nil)
}

func (e *generator) createApp() {
	t := time.Now().UnixNano()
	app := &ct.App{Name: fmt.Sprintf("my-app-%d", t)}
	err := e.client.CreateApp(app)
	if err == nil {
		e.resourceIds["app"] = app.ID
		e.resourceIds["app-name"] = app.Name
	}
}

func (e *generator) createAppError() {
	// create an invalid app
	// this should return a validation error
	e.client.CreateApp(&ct.App{
		Name: "this is not valid",
	})
}

func (e *generator) getInitialAppRelease() {
	appRelease, err := e.client.GetAppRelease("gitreceive")
	if err != nil {
		return
	}
	if artifact, err := e.client.GetArtifact(appRelease.Env["SLUGRUNNER_IMAGE_ID"]); err == nil {
		e.resourceIds["SLUGRUNNER_IMAGE_URI"] = artifact.URI
	}
}

func (e *generator) getApp() {
	e.client.GetApp(e.resourceIds["app"])
}

func (e *generator) listApps() {
	e.client.AppList()
}

func (e *generator) updateApp() {
	app := &ct.App{
		ID: e.resourceIds["app"],
		Meta: map[string]string{
			"bread": "with hemp",
		},
	}
	e.client.UpdateApp(app)
}

func (e *generator) getAppLog() {
	app, err := e.client.GetApp("controller")
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["controller"] = app.ID // save ID for streamAppLog
	e.recorder.GetRequests()             // discard above request
	lines := 10
	res, err := e.client.GetAppLog(app.ID, &logagg.LogOpts{
		Lines: &lines,
	})
	if err == nil {
		defer res.Close()
		io.Copy(ioutil.Discard, res)
	}
}

func (e *generator) streamAppLog() {
	output := make(chan *ct.SSELogChunk)
	lines := 10
	e.client.StreamAppLog(e.resourceIds["controller"], &logagg.LogOpts{
		Lines: &lines,
	}, output)
	timeout := time.After(10 * time.Second)
outer:
	for {
		select {
		case <-output:
		case <-timeout:
			break outer
		}
	}
}

func (e *generator) listAppResources() {
	e.client.AppResourceList(e.resourceIds["app"])
}

func (e *generator) createRoute() {
	route := (&router.HTTPRoute{
		Domain:  "http://example.com",
		Service: e.resourceIds["app-name"] + "-web",
	}).ToRoute()
	err := e.client.CreateRoute(e.resourceIds["app"], route)
	if err == nil {
		e.resourceIds["route"] = route.FormattedID()
	}
}

func (e *generator) getRoute() {
	e.client.GetRoute(e.resourceIds["app"], e.resourceIds["route"])
}

func (e *generator) updateRoute() {
	route, err := e.client.GetRoute(e.resourceIds["app"], e.resourceIds["route"])
	if err != nil {
		log.Fatal(err)
	}
	e.recorder.GetRequests() // discard above request
	route.Service = e.resourceIds["app-name"] + "-other"
	route.Sticky = true
	e.client.UpdateRoute(e.resourceIds["app"], e.resourceIds["route"], route)
}

func (e *generator) listRoutes() {
	e.client.RouteList(e.resourceIds["app"])
}

func (e *generator) deleteRoute() {
	e.client.DeleteRoute(e.resourceIds["app"], e.resourceIds["route"])
}

func (e *generator) deleteApp() {
	// call Delete rather than DeleteApp as the latter uses the app stream
	// to watch app_deletion events.
	e.client.(*v1controller.Client).Delete(fmt.Sprintf("/apps/%s", e.resourceIds["app"]), nil)
}

func (e *generator) createArtifact() {
	manifest := &ct.ImageManifest{
		Type: ct.ImageManifestTypeV1,
		Meta: map[string]string{"foo": "bar"},
		Entrypoints: map[string]*ct.ImageEntrypoint{
			"_default": {
				Env:        map[string]string{"key": "default-val"},
				WorkingDir: "/",
				Args:       []string{"bash"},
			},
			"web": {
				Env:        map[string]string{"key": "other-val"},
				WorkingDir: "/app",
				Args:       []string{"/bin/web-server"},
				Uid:        typeconv.Uint32Ptr(1000),
				Gid:        typeconv.Uint32Ptr(1000),
			},
		},
		Rootfs: []*ct.ImageRootfs{
			{
				Platform: &ct.ImagePlatform{
					Architecture: "amd64",
					OS:           "linux",
				},
				Layers: []*ct.ImageLayer{
					{
						ID:     "34510d7fb6c0b108121b5f5f2e86f0c4c27a1e6d1dbbbd131189a65fab641775",
						Type:   ct.ImageLayerTypeSquashfs,
						Length: 34570240,
						Hashes: map[string]string{"sha512_256": "34510d7fb6c0b108121b5f5f2e86f0c4c27a1e6d1dbbbd131189a65fab641775"},
					},
					{
						ID:     "d038205ed955b94a475dfaf81b4e593b77d3b74420d673fb0bf0a7ca8c4cc345",
						Type:   ct.ImageLayerTypeSquashfs,
						Length: 92368896,
						Hashes: map[string]string{"sha512_256": "d038205ed955b94a475dfaf81b4e593b77d3b74420d673fb0bf0a7ca8c4cc345"},
					},
				},
			},
		},
	}
	artifact := &ct.Artifact{
		Type:             ct.ArtifactTypeFlynn,
		URI:              e.resourceIds["SLUGRUNNER_IMAGE_URI"],
		RawManifest:      manifest.RawManifest(),
		Hashes:           manifest.Hashes(),
		Size:             int64(len(manifest.RawManifest())),
		LayerURLTemplate: "https://dl.flynn.io/tuf?target=/layers/{id}.squashfs",
	}
	err := e.client.CreateArtifact(artifact)
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["artifact"] = artifact.ID
}

func (e *generator) listArtifacts() {
	e.client.ArtifactList()
}

func (e *generator) createRelease() {
	release := &ct.Release{
		ArtifactIDs: []string{e.resourceIds["artifact"]},
		Env: map[string]string{
			"some": "info",
		},
		Processes: map[string]ct.ProcessType{
			"foo": {
				Args: []string{"ls", "-l"},
				Env: map[string]string{
					"BAR": "baz",
				},
			},
		},
	}
	err := e.client.CreateRelease(e.resourceIds["app"], release)
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["release"] = release.ID
}

func (e *generator) listReleases() {
	e.client.ReleaseList()
}

func (e *generator) listAppReleases() {
	e.client.AppReleaseList(e.resourceIds["app"])
}

func (e *generator) getAppRelease() {
	e.client.GetAppRelease(e.resourceIds["app"])
}

func (e *generator) setAppRelease() {
	e.client.SetAppRelease(e.resourceIds["app"], e.resourceIds["release"])
}

func (e *generator) putFormation() {
	formation := &ct.Formation{
		AppID:     e.resourceIds["app"],
		ReleaseID: e.resourceIds["release"],
		Processes: map[string]int{
			"foo": 1,
		},
	}
	e.client.PutFormation(formation)
}

func (e *generator) getFormation() {
	e.client.GetFormation(e.resourceIds["app"], e.resourceIds["release"])
}

func (e *generator) getExpandedFormation() {
	e.client.GetExpandedFormation(e.resourceIds["app"], e.resourceIds["release"])
}

func (e *generator) listFormations() {
	e.client.FormationList(e.resourceIds["app"])
}

func (e *generator) listActiveFormations() {
	e.client.FormationListActive()
}

func (e *generator) streamFormations() {
	output := make(chan *ct.ExpandedFormation)
	e.client.StreamFormations(nil, output)
	timeout := time.After(10 * time.Second)
outer:
	for {
		select {
		case <-output:
		case <-timeout:
			break outer
		}
	}
}

func (e *generator) deleteFormation() {
	e.client.DeleteFormation(e.resourceIds["app"], e.resourceIds["release"])
}

func (e *generator) runJob() {
	new_job := &ct.NewJob{
		ReleaseID: e.resourceIds["release"],
		Env: map[string]string{
			"BODY": "Hello!",
		},
		Args: []string{"echo", "$BODY"},
	}
	job, err := e.client.RunJobDetached(e.resourceIds["app"], new_job)
	if err == nil {
		e.resourceIds["job"] = job.UUID
	}
}

func (e *generator) listJobs() {
	e.client.JobList(e.resourceIds["app"])
}

func (e *generator) updateJob() {
	job := &ct.Job{
		UUID:      e.resourceIds["job"],
		AppID:     e.resourceIds["app"],
		ReleaseID: e.resourceIds["release"],
		State:     "down",
	}
	e.client.PutJob(job)
}

func (e *generator) getJob() {
	e.client.GetJob(e.resourceIds["app"], e.resourceIds["job"])
}

func (e *generator) deleteJob() {
	e.client.DeleteJob(e.resourceIds["app"], e.resourceIds["job"])
}

func (e *generator) createProvider() {
	t := time.Now().UnixNano()
	provider := &ct.Provider{
		Name: fmt.Sprintf("example-provider-%d", t),
		URL:  fmt.Sprintf("http://example-provider-%d.discoverd:%s/providers/%d", t, e.conf.ourPort, t),
	}
	err := e.client.CreateProvider(provider)
	if err != nil {
		log.Fatal(err)
	}
	_, err = discoverd.AddServiceAndRegister(provider.Name, ":"+e.conf.ourPort)
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["provider"] = provider.ID
}

func (e *generator) getProvider() {
	e.client.GetProvider(e.resourceIds["provider"])
}

func (e *generator) listProviders() {
	e.client.ProviderList()
}

func (e *generator) createProviderResource() {
	resourceConfig := json.RawMessage(`{}`)
	resourceReq := &ct.ResourceReq{
		ProviderID: e.resourceIds["provider"],
		Config:     &resourceConfig,
	}
	resource, err := e.client.ProvisionResource(resourceReq)
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["provider_resource"] = resource.ID
}

func (e *generator) putProviderResource() {
	resource := &ct.Resource{
		ID:         random.UUID(),
		ProviderID: e.resourceIds["provider"],
		ExternalID: "/foo/bar",
		Env:        map[string]string{"FOO": "BAR"},
		Apps: []string{
			e.resourceIds["app"],
		},
	}
	e.client.PutResource(resource)
}

func (e *generator) getProviderResource() {
	providerID := e.resourceIds["provider"]
	resourceID := e.resourceIds["provider_resource"]
	e.client.GetResource(providerID, resourceID)
}

func (e *generator) listProviderResources() {
	e.client.ResourceList(e.resourceIds["provider"])
}

func (e *generator) deleteProviderResource() {
	e.client.DeleteResource(e.resourceIds["provider"], e.resourceIds["resource"])
}

func (e *generator) createDeployment() {
	deployment, err := e.client.CreateDeployment(e.resourceIds["app"], e.resourceIds["release"])
	if err != nil {
		log.Fatal(err)
	}
	e.resourceIds["deployment"] = deployment.ID
}

func (e *generator) getDeployment() {
	e.client.GetDeployment(e.resourceIds["deployment"])
}

func (e *generator) listDeployments() {
	e.client.DeploymentList(e.resourceIds["app"])
}

func (e *generator) eventsList() {
	events, err := e.client.ListEvents(ct.ListEventsOptions{
		Count: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(events) == 0 {
		log.Fatal(fmt.Errorf("events_list: expected there to be more than zero events"))
	}
	e.resourceIds["event"] = strconv.FormatInt(events[0].ID, 10)
}

func (e *generator) eventsStream() {
	events := make(chan *ct.Event)
	e.client.StreamEvents(ct.StreamEventsOptions{
		Past:  true,
		Count: 10,
	}, events)
	timeout := time.After(10 * time.Second)
outer:
	for {
		select {
		case <-events:
		case <-timeout:
			break outer
		}
	}
}

func (e *generator) eventGet() {
	eventID, err := strconv.ParseInt(e.resourceIds["event"], 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	e.client.GetEvent(eventID)
}

func (e *generator) getCACert() {
	e.client.GetCACert()
}

func (e *generator) clusterBackup() {
	// don't read response so it's not shown in docs
	e.client.Backup()
}

func (e *generator) migrateClusterDomain() {
	release, err := e.client.GetAppRelease("controller")
	if err != nil {
		log.Fatal(err)
	}
	oldDomain := release.Env["DEFAULT_ROUTE_DOMAIN"]
	e.recorder.GetRequests() // ignore above request
	err = e.client.PutDomain(&ct.DomainMigration{
		Domain:    "127.0.0.1.xip.io",
		OldDomain: oldDomain,
	})
	if err != nil {
		log.Fatal(err)
	}
}
