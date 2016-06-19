package main

import (
	"crypto/subtle"
	"net/http"

	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/auth"
)

func init() {
	auth.Register("flynn", auth.InitFunc(newAuth))
}

func newAuth(options map[string]interface{}) (auth.AccessController, error) {
	return &Auth{key: options["auth_key"].(string)}, nil
}

type Auth struct {
	key string
}

// Authorized implements the auth.AccessController interface and authorizes a
// request if it includes the correct auth key
func (a *Auth) Authorized(ctx context.Context, accessRecords ...auth.Access) (context.Context, error) {
	req, err := context.GetRequest(ctx)
	if err != nil {
		return nil, err
	}
	_, password, _ := req.BasicAuth()
	if password == "" {
		password = req.URL.Query().Get("key")
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.key)) != 1 {
		return nil, Challenge{}
	}
	return ctx, nil
}

type Challenge struct{}

func (Challenge) SetHeaders(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Basic realm=docker-receive")
}

func (Challenge) Error() string {
	return "basic authentication failed"
}
