package jsonschema

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

type maxLength int

func (m maxLength) Validate(v interface{}) []ValidationError {
	l, ok := v.(string)
	if !ok {
		return nil
	}
	if utf8.RuneCountInString(l) > int(m) {
		lenErr := ValidationError{fmt.Sprintf("String length must be shorter than %d characters.", m)}
		return []ValidationError{lenErr}
	}
	return nil
}

type minLength int

func (m minLength) Validate(v interface{}) []ValidationError {
	l, ok := v.(string)
	if !ok {
		return nil
	}
	if utf8.RuneCountInString(l) < int(m) {
		lenErr := ValidationError{fmt.Sprintf("String length must be shorter than %d characters.", m)}
		return []ValidationError{lenErr}
	}
	return nil
}

type pattern struct {
	regexp.Regexp
}

func (p *pattern) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	r, err := regexp.Compile(s)
	if err != nil {
		return err
	}
	p.Regexp = *r
	return nil
}

func (p pattern) Validate(v interface{}) []ValidationError {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !p.MatchString(s) {
		patErr := ValidationError{fmt.Sprintf("String must match the pattern: \"%s\".", p.String())}
		return []ValidationError{patErr}
	}
	return nil
}

type format string

var dateTimeRegexp = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})([Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?)?([Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})(\\.[0-9]+)?)?(([Zz]|([+-])([0-9]{2}):([0-9]{2})))?`)
var mailRegexp = regexp.MustCompile(".+@.+")
var hostnameRegexp = regexp.MustCompile(`^[a-zA-Z](([-0-9a-zA-Z]+)?[0-9a-zA-Z])?(\.[a-zA-Z](([-0-9a-zA-Z]+)?[0-9a-zA-Z])?)*$`)

func (f format) Validate(v interface{}) []ValidationError {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	switch f {
	case "date-time":
		if !dateTimeRegexp.MatchString(s) {
			return []ValidationError{{"Value must conform to RFC3339."}}
		}
	case "uri":
		if _, err := url.ParseRequestURI(s); err != nil {
			return []ValidationError{{"Value must be a valid URI, according to RFC3986."}}
		}
	case "email":
		if !mailRegexp.MatchString(s) {
			return []ValidationError{{"Value must be a valid email address, according to RFC5322."}}
		}
	case "ipv4":
		if net.ParseIP(s).To4() == nil {
			return []ValidationError{{"Value must be a valid IPv4 address."}}
		}
	case "ipv6":
		if net.ParseIP(s).To16() == nil {
			return []ValidationError{{"Value must be a valid IPv6 address."}}
		}
	case "hostname":
		formatErr := []ValidationError{{"Value must be a valid hostname."}}
		if !hostnameRegexp.MatchString(s) || utf8.RuneCountInString(s) > 255 {
			return formatErr
		}
		labels := strings.Split(s, ".")
		for _, label := range labels {
			if utf8.RuneCountInString(label) > 63 {
				return formatErr
			}
		}
	}
	return nil
}
