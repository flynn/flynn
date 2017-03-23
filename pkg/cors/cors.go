// Forked from https://github.com/martini-contrib/cors

// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cors provides handlers to enable CORS support.
package cors

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	headerAllowOrigin      = "Access-Control-Allow-Origin"
	headerAllowCredentials = "Access-Control-Allow-Credentials"
	headerAllowHeaders     = "Access-Control-Allow-Headers"
	headerAllowMethods     = "Access-Control-Allow-Methods"
	headerExposeHeaders    = "Access-Control-Expose-Headers"
	headerMaxAge           = "Access-Control-Max-Age"

	headerOrigin         = "Origin"
	headerRequestMethod  = "Access-Control-Request-Method"
	headerRequestHeaders = "Access-Control-Request-Headers"
)

var (
	defaultAllowHeaders = []string{"Origin", "Accept", "Content-Type", "Authorization"}
	// Regex patterns are generated from AllowOrigins. These are used and generated internally.
	allowOriginPatterns = []string{}
)

// Represents Access Control options.
type Options struct {
	// If set, all origins are allowed.
	AllowAllOrigins bool
	// A list of allowed origins. Wild cards and FQDNs are supported.
	AllowOrigins []string
	// A func for determining if `origin` is allowed at request time
	ShouldAllowOrigin func(origin string, req *http.Request) bool
	// If set, allows to share auth credentials such as cookies.
	AllowCredentials bool
	// A list of allowed HTTP methods.
	AllowMethods []string
	// A list of allowed HTTP headers.
	AllowHeaders []string
	// A list of exposed HTTP headers.
	ExposeHeaders []string
	// Max age of the CORS headers.
	MaxAge time.Duration
}

// Converts options into CORS headers.
func (o *Options) Header(origin string, req *http.Request) (headers map[string]string) {
	headers = make(map[string]string)
	// if origin is not allowed, don't extend the headers
	// with CORS headers.
	if !o.AllowAllOrigins && !o.IsOriginAllowed(origin, req) {
		return
	}

	// add allow origin
	headers[headerAllowOrigin] = origin

	// add allow credentials
	headers[headerAllowCredentials] = strconv.FormatBool(o.AllowCredentials)

	// add allow methods
	if len(o.AllowMethods) > 0 {
		headers[headerAllowMethods] = strings.Join(o.AllowMethods, ",")
	}

	// add allow headers
	if len(o.AllowHeaders) > 0 {
		// TODO: Add default headers
		headers[headerAllowHeaders] = strings.Join(o.AllowHeaders, ",")
	}

	// add exposed header
	if len(o.ExposeHeaders) > 0 {
		headers[headerExposeHeaders] = strings.Join(o.ExposeHeaders, ",")
	}
	// add a max age header
	if o.MaxAge > time.Duration(0) {
		headers[headerMaxAge] = strconv.FormatInt(int64(o.MaxAge/time.Second), 10)
	}
	return
}

// Looks up if the origin matches one of the patterns
// generated from Options.AllowOrigins patterns.
func (o *Options) IsOriginAllowed(origin string, req *http.Request) (allowed bool) {
	if o.ShouldAllowOrigin != nil {
		return o.ShouldAllowOrigin(origin, req)
	}
	for _, pattern := range allowOriginPatterns {
		allowed, _ = regexp.MatchString(pattern, origin)
		if allowed {
			return
		}
	}
	return
}

// Allows CORS for requests those match the provided options.
func (o *Options) Handler(next http.Handler) http.HandlerFunc {
	// Allow default headers if nothing is specified.
	if len(o.AllowHeaders) == 0 {
		o.AllowHeaders = defaultAllowHeaders
	}

	for _, origin := range o.AllowOrigins {
		pattern := regexp.QuoteMeta(origin)
		pattern = strings.Replace(pattern, "\\*", ".*", -1)
		pattern = strings.Replace(pattern, "\\?", ".", -1)
		allowOriginPatterns = append(allowOriginPatterns, "^"+pattern+"$")
	}

	return func(w http.ResponseWriter, req *http.Request) {
		if origin := req.Header.Get(headerOrigin); origin != "" {
			for key, value := range o.Header(origin, req) {
				w.Header().Set(key, value)
			}
			if req.Method == "OPTIONS" {
				w.WriteHeader(200)
				return
			}
		}
		next.ServeHTTP(w, req)
	}
}
