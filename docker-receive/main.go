package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution"
	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/registry/handlers"
	"github.com/docker/distribution/registry/middleware/repository"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/docker-receive/blobstore"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/version"
)

// main is a modified version of the registry main function:
// https://github.com/docker/distribution/blob/6ba799b/cmd/registry/main.go
func main() {
	logrus.SetLevel(logrus.InfoLevel)

	ctx := context.Background()
	ctx = context.WithValue(ctx, "version", version.String())
	ctx = context.WithLogger(ctx, context.GetLogger(ctx, "version"))

	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		context.GetLogger(ctx).Fatalln(err)
	}

	authKey := os.Getenv("AUTH_KEY")

	middleware.Register("flynn", repositoryMiddleware(client, authKey))

	config := configuration.Configuration{
		Version: configuration.CurrentVersion,
		Storage: configuration.Storage{
			blobstore.DriverName: configuration.Parameters{},
			"delete":             configuration.Parameters{"enabled": true},
		},
		Middleware: map[string][]configuration.Middleware{
			"repository": {
				{Name: "flynn"},
			},
		},
		Auth: configuration.Auth{
			"flynn": configuration.Parameters{
				"auth_key": authKey,
			},
		},
	}
	config.HTTP.Secret = os.Getenv("REGISTRY_HTTP_SECRET")

	status.AddHandler(status.HealthyHandler)

	app := handlers.NewApp(ctx, config)
	http.Handle("/", app)

	addr := ":" + os.Getenv("PORT")
	context.GetLogger(app).Infof("listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		context.GetLogger(app).Fatalln(err)
	}
}

func repositoryMiddleware(client controller.Client, authKey string) middleware.InitFunc {
	return func(ctx context.Context, r distribution.Repository, _ map[string]interface{}) (distribution.Repository, error) {
		return &repository{
			Repository: r,
			client:     client,
			authKey:    authKey,
		}, nil
	}
}

// repository is a repository middleware which returns a custom ManifestService
// in order to create Flynn artifacts when image manifests are pushed
type repository struct {
	distribution.Repository

	client  controller.Client
	authKey string
}

func (r *repository) Manifests(ctx context.Context, options ...distribution.ManifestServiceOption) (distribution.ManifestService, error) {
	m, err := r.Repository.Manifests(ctx, options...)
	if err != nil {
		return nil, err
	}
	return &manifestService{
		ManifestService: m,
		repository:      r,
		client:          r.client,
		authKey:         r.authKey,
	}, nil
}

type manifestService struct {
	distribution.ManifestService

	repository distribution.Repository
	client     controller.Client
	authKey    string
}

func (m *manifestService) Put(manifest *manifest.SignedManifest) error {
	if err := m.ManifestService.Put(manifest); err != nil {
		return err
	}

	dgst, err := digestManifest(manifest)
	if err != nil {
		return err
	}

	return m.createArtifact(dgst)
}

func (m *manifestService) createArtifact(dgst digest.Digest) error {
	return m.client.CreateArtifact(&ct.Artifact{
		Type: host.ArtifactTypeDocker,
		URI:  fmt.Sprintf("http://flynn:%s@docker-receive.discoverd?name=%s&id=%s", m.authKey, m.repository.Name(), dgst),
		Meta: map[string]string{
			"docker-receive.repository": m.repository.Name(),
			"docker-receive.digest":     string(dgst),
		},
	})
}

// digestManifest is a modified version of:
// https://github.com/docker/distribution/blob/6ba799b/registry/handlers/images.go#L228-L251
func digestManifest(manifest *manifest.SignedManifest) (digest.Digest, error) {
	p, err := manifest.Payload()
	if err != nil {
		return "", err
	}
	return digest.FromBytes(p)
}
