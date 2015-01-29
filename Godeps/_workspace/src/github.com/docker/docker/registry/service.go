package registry

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/engine"
)

// Service exposes registry capabilities in the standard Engine
// interface. Once installed, it extends the engine with the
// following calls:
//
//  'auth': Authenticate against the public registry
//  'search': Search for images on the public registry
//  'pull': Download images from any registry (TODO)
//  'push': Upload images to any registry (TODO)
type Service struct {
	insecureRegistries []string
}

// NewService returns a new instance of Service ready to be
// installed no an engine.
func NewService(insecureRegistries []string) *Service {
	return &Service{
		insecureRegistries: insecureRegistries,
	}
}

// Install installs registry capabilities to eng.
func (s *Service) Install(eng *engine.Engine) error {
	eng.Register("auth", s.Auth)
	eng.Register("search", s.Search)
	return nil
}

// Auth contacts the public registry with the provided credentials,
// and returns OK if authentication was sucessful.
// It can be used to verify the validity of a client's credentials.
func (s *Service) Auth(job *engine.Job) engine.Status {
	var authConfig = new(AuthConfig)

	job.GetenvJson("authConfig", authConfig)

	if addr := authConfig.ServerAddress; addr != "" && addr != IndexServerAddress() {
		endpoint, err := NewEndpoint(addr, s.insecureRegistries)
		if err != nil {
			return job.Error(err)
		}
		if _, err := endpoint.Ping(); err != nil {
			return job.Error(err)
		}
		authConfig.ServerAddress = endpoint.String()
	}

	status, err := Login(authConfig, HTTPRequestFactory(nil))
	if err != nil {
		return job.Error(err)
	}
	job.Printf("%s\n", status)

	return engine.StatusOK
}

// Search queries the public registry for images matching the specified
// search terms, and returns the results.
//
// Argument syntax: search TERM
//
// Option environment:
//	'authConfig': json-encoded credentials to authenticate against the registry.
//		The search extends to images only accessible via the credentials.
//
//	'metaHeaders': extra HTTP headers to include in the request to the registry.
//		The headers should be passed as a json-encoded dictionary.
//
// Output:
//	Results are sent as a collection of structured messages (using engine.Table).
//	Each result is sent as a separate message.
//	Results are ordered by number of stars on the public registry.
func (s *Service) Search(job *engine.Job) engine.Status {
	if n := len(job.Args); n != 1 {
		return job.Errorf("Usage: %s TERM", job.Name)
	}
	var (
		term        = job.Args[0]
		metaHeaders = map[string][]string{}
		authConfig  = &AuthConfig{}
	)
	job.GetenvJson("authConfig", authConfig)
	job.GetenvJson("metaHeaders", metaHeaders)

	hostname, term, err := ResolveRepositoryName(term)
	if err != nil {
		return job.Error(err)
	}

	endpoint, err := NewEndpoint(hostname, s.insecureRegistries)
	if err != nil {
		return job.Error(err)
	}
	r, err := NewSession(authConfig, HTTPRequestFactory(metaHeaders), endpoint, true)
	if err != nil {
		return job.Error(err)
	}
	results, err := r.SearchRepositories(term)
	if err != nil {
		return job.Error(err)
	}
	outs := engine.NewTable("star_count", 0)
	for _, result := range results.Results {
		out := &engine.Env{}
		out.Import(result)
		outs.Add(out)
	}
	outs.ReverseSort()
	if _, err := outs.WriteListTo(job.Stdout); err != nil {
		return job.Error(err)
	}
	return engine.StatusOK
}
