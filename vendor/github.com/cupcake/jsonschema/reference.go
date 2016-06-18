package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Using a pointer allows us to handle recursive embedded schemas.
type EmbeddedSchemas map[string]*Schema

func (e *EmbeddedSchemas) UnmarshalJSON(b []byte) error {
	*e = make(EmbeddedSchemas)
	err1 := e.UnmarshalArray(b)
	err2 := e.UnmarshalObject(b)
	err3 := e.UnmarshalSingle(b)
	if err1 != nil && err2 != nil && err3 != nil {
		return errors.New("no valid embedded schemas")
	}
	return nil
}

func (e *EmbeddedSchemas) UnmarshalArray(b []byte) error {
	var schemas []*Schema
	if err := json.Unmarshal(b, &schemas); err != nil {
		return err
	}
	for i, v := range schemas {
		(*e)[strconv.Itoa(i)] = v
	}
	return nil
}

func (e *EmbeddedSchemas) UnmarshalObject(b []byte) error {
	var m map[string]*Schema
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		(*e)[k] = v
	}
	return nil
}

func (e *EmbeddedSchemas) UnmarshalSingle(b []byte) error {
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	(*e)[""] = &s
	return nil
}

// ResolveRefs starts a depth-first search through a document for schemas containing
// the 'ref' validator. It completely resolves each one found.
func (s *Schema) ResolveRefs(loadExternal bool) {
	s.resolveSelfAndBelow(*s, *s, loadExternal)
}

func (s *Schema) resolveSelfAndBelow(parentSchema, rootSchema Schema, loadExternal bool) {
	if parentSchema.id != "" && parentSchema.id != s.id {
		s.parentId = parentSchema.id
		sURL, sURLErr := url.Parse(s.id)
		pURL, pURLErr := url.Parse(parentSchema.id)
		if sURLErr == nil && pURLErr == nil && pURL.IsAbs() && !sURL.IsAbs() {
			if strings.HasPrefix(s.id, "#") {
				pURL.Fragment = strings.TrimPrefix(s.id, "#")
			} else {
				s.id = pURL.ResolveReference(sURL).String()
			}
		}
	}
	cacheKey, err := resolveCacheKey(s.id)
	if err == nil {
		if _, ok := rootSchema.Cache[cacheKey]; !ok {
			rootSchema.Cache[cacheKey] = s
		}
	}
	s.resolveSelf(rootSchema, loadExternal)
	s.resolveBelow(rootSchema, loadExternal)
}

func (s *Schema) resolveSelf(rootSchema Schema, loadExternal bool) {
	if str, ok := s.hasRef(); ok {
		sch, err := s.refToSchema(str, rootSchema, loadExternal)
		if err != nil {
			return
		}
		*s = *sch
		s.resolveSelf(rootSchema, loadExternal)
	}
}

// TODO: test that we fail gracefully if the schema contains infinitely looping "$ref"s.
func (s *Schema) resolveBelow(rootSchema Schema, loadExternal bool) {
	if s.resolved == true {
		return
	}
	s.resolved = true
	for _, n := range s.nodes {
		for _, sch := range n.EmbeddedSchemas {
			sch.resolveSelfAndBelow(*s, rootSchema, loadExternal)
		}
	}
}

func (s *Schema) hasRef() (string, bool) {
	for _, n := range s.nodes {
		if r, ok := n.Validator.(*ref); ok {
			return string(*r), true
		}
	}
	return "", false
}

// TODO: This is hacky. Look into using a library like gojsonpointer[1] instead.
//
// [1] https://github.com/xeipuuv/gojsonpointer
func (s *Schema) refToSchema(str string, rootSchema Schema, loadExternal bool) (*Schema, error) {
	parentURL, err := url.Parse(s.parentId)
	if err == nil && parentURL.IsAbs() {
		sURL, err := url.Parse(str)
		if err == nil && !sURL.IsAbs() && !strings.HasPrefix(str, "#") {
			str = parentURL.ResolveReference(sURL).String()
		}
	}

	var split []string
	url, err := url.Parse(str)
	cacheKey, cacheKeyErr := resolveCacheKey(str)
	if err == nil && cacheKeyErr == nil {
		cachedSchema, ok := rootSchema.Cache[cacheKey]
		if ok {
			rootSchema = *cachedSchema
		} else {
			// Handle external URIs.
			if !loadExternal {
				return new(Schema), errors.New("external schemas are disabled")
			}
			resp, err := http.Get(str)
			if err != nil {
				return new(Schema), errors.New("bad external url")
			}
			defer resp.Body.Close()
			s, err := ParseWithCache(resp.Body, loadExternal, &rootSchema.Cache)
			if err != nil {
				return new(Schema), errors.New("error parsing external doc")
			}
			rootSchema.Cache[cacheKey] = s
			rootSchema = *s
		}
		str = url.Fragment
	}

	// Remove the prefix from internal URIs.
	str = strings.TrimPrefix(str, "#")
	str = strings.TrimPrefix(str, "/")

	split = strings.Split(str, "/")
	// Make replacements.
	for i, v := range split {
		r := strings.NewReplacer("~0", "~", "~1", "/", "%25", "%")
		split[i] = r.Replace(v)
	}
	// Resolve the local part of the URI.
	return resolveLocalPath(split, rootSchema, str)
}

// TODO: add code and tests for references more than one level deep.
func resolveLocalPath(split []string, rootSchema Schema, str string) (*Schema, error) {
	switch len(split) {
	case 1:
		if split[0] == "" {
			return &rootSchema, nil
		}
		v, ok := rootSchema.nodes[split[0]]
		if ok == false {
			break
		}
		if s, ok := v.EmbeddedSchemas[""]; ok {
			return s, nil
		}
	case 2:
		v, ok := rootSchema.nodes[split[0]]
		if ok == false {
			break
		}
		if s, ok := v.EmbeddedSchemas[split[1]]; ok {
			return s, nil
		}
	}
	return new(Schema), fmt.Errorf("failed to resolve %s", str)
}

func resolveCacheKey(id string) (string, error) {
	url, err := url.Parse(id)
	if err == nil && url.IsAbs() {
		url.Fragment = ""
		return strings.TrimSuffix(url.String(), "/"), nil
	}
	return "", fmt.Errorf("failed to resolve cache key for %s", id)
}
