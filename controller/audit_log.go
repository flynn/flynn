package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/router/types"
	log "github.com/inconshreveable/log15"
)

const maxAuditRequestBodySize = 1000000

func handleRequestWithAuditBodyBuffer(h http.Handler, rw *httphelper.ResponseWriter, req *http.Request) *bytes.Buffer {
	var buf *bytes.Buffer
	if req.Body != nil && strings.Contains(req.Header.Get("Content-Type"), "application/json") {
		buf = &bytes.Buffer{}
		req.Body = struct {
			io.Reader
			io.Closer
		}{
			io.TeeReader(&limitedBodyReader{R: req.Body, N: maxAuditRequestBodySize}, buf),
			req.Body,
		}
	}
	h.ServeHTTP(rw, req)
	maybeRedactBody(req, buf)
	return buf
}

type limitedBodyReader struct {
	R io.Reader // underlying reader
	N int64     // max bytes remaining
}

const redactedPlaceholder = "[redacted]"

func redactEnv(env map[string]string) {
	for k, v := range env {
		for _, s := range []string{"key", "token", "pass", "secret", "url"} {
			if v != "" && strings.Contains(strings.ToLower(k), s) {
				env[k] = redactedPlaceholder
				break
			}
		}
	}
}

func maybeRedactBody(req *http.Request, buf *bytes.Buffer) {
	if buf == nil || buf.Len() == 0 {
		return
	}
	var data interface{}
	if req.Method == "POST" && req.URL.Path == "/releases" {
		release := &ct.Release{}
		if err := json.NewDecoder(buf).Decode(release); err != nil {
			return
		}
		redactEnv(release.Env)
		for _, proc := range release.Processes {
			redactEnv(proc.Env)
		}
		data = release
	} else if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/jobs") {
		job := &ct.NewJob{}
		if err := json.NewDecoder(buf).Decode(job); err != nil {
			return
		}
		redactEnv(job.Env)
		data = job
	} else if (req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/routes")) || (req.Method == "PUT" && strings.Contains(req.URL.Path, "/routes/")) {
		route := &router.Route{}
		if err := json.NewDecoder(buf).Decode(route); err != nil {
			return
		}
		if route.Type != "http" {
			return
		}
		if route.Certificate != nil && route.Certificate.Key != "" {
			route.Certificate.Key = redactedPlaceholder
		}
		if route.LegacyTLSKey != "" {
			route.LegacyTLSKey = redactedPlaceholder
		}
		data = route
	}
	if data != nil {
		buf.Reset()
		json.NewEncoder(buf).Encode(data)
	}
}

func (l *limitedBodyReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, httphelper.ErrRequestBodyTooBig
	}
	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}
	n, err = l.R.Read(p)
	l.N -= int64(n)
	return
}

func auditLoggerFn(handler http.Handler, logger log.Logger, clientIP string, rw *httphelper.ResponseWriter, req *http.Request) {
	start := time.Now()
	logger.Info("request started", "method", req.Method, "path", req.URL.Path, "client_ip", clientIP)
	bodyBuf := handleRequestWithAuditBodyBuffer(handler, rw, req)
	logger.Info("request completed", "status", rw.Status(), "duration", time.Since(start), "method", req.Method, "path", req.URL.Path, "client_ip", clientIP, "key_id", req.Header.Get("Flynn-Auth-Key-ID"), "user_agent", req.Header.Get("User-Agent"), "body", bodyBuf.String())
}
