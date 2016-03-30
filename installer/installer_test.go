package installer

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flynn/flynn/pkg/installer"
	"github.com/flynn/flynn/pkg/random"
	. "github.com/flynn/go-check"
)

type TestCluster struct {
	Type string `json:"type"`
}

func NewTestCluster() *TestCluster {
	return &TestCluster{Type: "test"}
}

type TestPrompt struct {
	UUID     string
	Response []byte
}

func (p *TestPrompt) ID() string {
	return p.UUID
}

func (p *TestPrompt) Respond(res io.Reader) {
	var data bytes.Buffer
	io.Copy(&data, res)
	p.Response = data.Bytes()
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	srv *httptest.Server
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	installer.Register("test", TestCluster{})

	handler := apiHandler()
	s.srv = httptest.NewServer(handler)
}

func (s *S) TestLaunchCluster(c *C) {
	cluster := NewTestCluster()
	var data bytes.Buffer
	err := json.NewEncoder(&data).Encode(cluster)
	c.Assert(err, IsNil)
	res, err := http.Post(s.srv.URL+"/clusters", "application/json", &data)
	c.Assert(err, IsNil)
	res.Body.Close()
	c.Assert(res.StatusCode, Equals, 200)
}

func (s *S) TestPromptResponse(c *C) {
	prompt := &TestPrompt{
		UUID: random.UUID(),
	}
	api.addPendingPrompt(prompt)
	payload := "testing prompt"
	data := bytes.NewReader([]byte(payload))
	_, err := http.Post(s.srv.URL+"/prompts/"+prompt.ID(), "", data)
	c.Assert(err, IsNil)
	c.Assert(string(prompt.Response), Equals, payload)

	// second attempt should fail with 404
	resp, _ := http.Post(s.srv.URL+"/prompts/"+prompt.ID(), "", data)
	c.Assert(resp.StatusCode, Equals, 404)
}
