package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	. "github.com/flynn/go-check"
	"github.com/gorilla/sessions"
)

const (
	testLoginToken    = "test-login-token"
	testControllerKey = "test-controller-key"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	srv        *httptest.Server
	cookiePath string
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	s.cookiePath = "/"
	s.srv = httptest.NewServer(APIHandler(&Config{
		SessionStore:  sessions.NewCookieStore([]byte("session-secret")),
		LoginToken:    testLoginToken,
		ControllerKey: testControllerKey,
		CookiePath:    s.cookiePath,
	}))
}

func (s *S) testAuthenticated(c *C, client *http.Client) {
	res, err := client.Get(s.srv.URL + "/config")
	c.Assert(err, IsNil)
	var conf *UserConfig
	c.Assert(json.NewDecoder(res.Body).Decode(&conf), IsNil)
	c.Assert(conf.User, Not(IsNil))
	c.Assert(conf.User.ControllerKey, Equals, testControllerKey)
}

func (s *S) TestUserSessionJSON(c *C) {
	loginInfo := LoginInfo{Token: testLoginToken}
	data, err := json.Marshal(&loginInfo)
	c.Assert(err, IsNil)
	jar, err := cookiejar.New(&cookiejar.Options{})
	c.Assert(err, IsNil)
	client := &http.Client{
		Jar: jar,
	}
	res, err := client.Post(s.srv.URL+"/user/sessions", "application/json", bytes.NewReader(data))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	s.testAuthenticated(c, client)
}

func (s *S) TestUserSessionForm(c *C) {
	data := url.Values{}
	data.Set("token", testLoginToken)
	jar, err := cookiejar.New(&cookiejar.Options{})
	c.Assert(err, IsNil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.PostForm(s.srv.URL+"/user/sessions", data)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 302)
	c.Assert(res.Header.Get("Location"), Equals, s.cookiePath)
	s.testAuthenticated(c, client)
}
