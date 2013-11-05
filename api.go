package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strings"
)

const userAgent = "flynn-cli dev " + runtime.GOOS + " " + runtime.GOARCH

func init() {
	if os.Getenv("FLYNN_SSL_VERIFY") == "disable" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
}

func Get(v interface{}, path string) error {
	return APIReq(v, "GET", path, nil)
}

func Patch(v interface{}, path string, body interface{}) error {
	return APIReq(v, "PATCH", path, body)
}

func Post(v interface{}, path string, body interface{}) error {
	return APIReq(v, "POST", path, body)
}

func Put(v interface{}, path string, body interface{}) error {
	return APIReq(v, "PUT", path, body)
}

func Delete(path string) error {
	return APIReq(nil, "DELETE", path, nil)
}

// Generates an HTTP request for the Flynn API, but does not
// perform the request. The request's Accept header field will be
// set to:
//
//   Accept: application/vnd.flynn+json; version=1
//
// The type of body determines how to encode the request:
//
//   nil         no body
//   io.Reader   body is sent verbatim
//   else        body is encoded as application/json
func NewRequest(method, path string, body interface{}) (*http.Request, error) {
	var ctype string
	var rbody io.Reader

	switch t := body.(type) {
	case nil:
	case io.Reader:
		rbody = t
	default:
		j, err := json.Marshal(body)
		if err != nil {
			log.Fatal(err)
		}
		rbody = bytes.NewReader(j)
		ctype = "application/json"
	}
	req, err := http.NewRequest(method, apiURL+path, rbody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Accept", "application/vnd.flynn+json; version=1")
	for _, h := range strings.Split(os.Getenv("FLYNN_HEADER"), "\n") {
		if i := strings.Index(h, ":"); i >= 0 {
			req.Header.Set(
				strings.TrimSpace(h[:i]),
				strings.TrimSpace(h[i+1:]),
			)
		}
	}
	return req, nil
}

// Sends a Flynn API request and decodes the response into v. As
// described in NewRequest(), the type of body determines how to
// encode the request body. As described in DoReq(), the type of
// v determines how to handle the response body.
func APIReq(v interface{}, meth, path string, body interface{}) error {
	req, err := NewRequest(meth, path, body)
	if err != nil {
		return err
	}
	return DoReq(req, v)
}

// Submits an HTTP request, checks its response, and deserializes
// the response into v. The type of v determines how to handle
// the response body:
//
//   nil        body is discarded
//   io.Writer  body is copied directly into v
//   else       body is decoded into v as json
//
func DoReq(req *http.Request, v interface{}) error {
	debug := os.Getenv("FLYNN_DEBUG") != ""
	if debug {
		dump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			log.Println(err)
		} else {
			os.Stderr.Write(dump)
			os.Stderr.Write([]byte{'\n', '\n'})
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if debug {
		dump, err := httputil.DumpResponse(res, true)
		if err != nil {
			log.Println(err)
		} else {
			os.Stderr.Write(dump)
			os.Stderr.Write([]byte{'\n'})
		}
	}
	if err = checkResp(res); err != nil {
		return err
	}
	switch t := v.(type) {
	case nil:
	case io.Writer:
		_, err = io.Copy(t, res.Body)
	default:
		err = json.NewDecoder(res.Body).Decode(v)
	}
	return err
}

func checkResp(res *http.Response) error {
	if res.StatusCode == 401 {
		return errors.New("Unauthorized")
	}
	if res.StatusCode == 403 {
		return errors.New("Unauthorized")
	}
	if res.StatusCode/100 != 2 { // 200, 201, 202, etc
		return errors.New("Unexpected error: " + res.Status)
	}
	if msg := res.Header.Get("Flynn-Warning"); msg != "" {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(msg))
	}
	return nil
}

var cmdAPI = &Command{
	Run:   runAPI,
	Usage: "api method path",
	Short: "make a single API request" + extra,
	Long: `
The api command is a convenient but low-level way to send requests
to the Flynn API. It sends an HTTP request to the Flynn API
using the given method on the given path, using stdin unmodified
as the request body. It prints the response unmodified on stdout.
Method GET doesn't read or send a request body.

Method name input will be upcased, so both 'flynn api GET /apps' and
'flynn api get /apps' are valid commands.

As with any flynn command, the behavior of flynn api is controlled by
various environment variables. See 'flynn help environ' for details.

Examples:

    $ flynn api GET /apps/myapp | jq .
    {
      "name": "myapp",
      "id": "app123@heroku.com",
      "created_at": "2011-11-11T04:17:13-00:00",
      …
    }

    $ export FLYNN_HEADER
    $ FLYNN_HEADER='
    Content-Type: application/x-www-form-urlencoded
    Accept: application/json
    '
    $ printf 'type=web&qty=2' | flynn api POST /apps/myapp/ps/scale
    2
`,
}

func runAPI(cmd *Command, args []string) {
	if len(args) != 2 {
		cmd.printUsage()
		os.Exit(2)
	}
	method := strings.ToUpper(args[0])
	var body io.Reader
	if method != "GET" {
		body = os.Stdin
	}
	if err := APIReq(os.Stdout, method, args[1], body); err != nil {
		log.Fatal(err)
	}
}
