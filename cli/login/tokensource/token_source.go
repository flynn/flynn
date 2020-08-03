package tokensource

import (
	"fmt"
	"sync"

	"github.com/flynn/flynn/cli/login/internal/oauth"
	"golang.org/x/oauth2"
)

func New(issuer, controllerURL string, cache Cache) (oauth2.TokenSource, error) {
	metadataURL, clientID, err := oauth.BuildMetadataURL(issuer)
	if err != nil {
		return nil, err
	}

	t, err := cache.GetToken(issuer, clientID, controllerURL)
	if err != nil {
		return nil, err
	}

	return &tokenSource{
		issuer:      issuer,
		metadataURL: metadataURL,
		cache:       cache,
		config:      &oauth2.Config{ClientID: clientID},
		t:           t,
	}, nil
}

type tokenSource struct {
	issuer      string
	metadataURL string
	cache       Cache

	mtx    sync.Mutex
	config *oauth2.Config
	t      *oauth2.Token
}

func (s *tokenSource) discover() error {
	meta, err := oauth.GetMetadata(s.metadataURL)
	if err != nil {
		return err
	}
	s.config.Endpoint = oauth2.Endpoint{
		AuthStyle: oauth2.AuthStyleInParams,
		AuthURL:   meta.AuthorizationEndpoint,
		TokenURL:  meta.TokenEndpoint,
	}
	return nil
}

func (s *tokenSource) Token() (*oauth2.Token, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.t.Valid() {
		return s.t, nil
	}

	if s.config.Endpoint.TokenURL == "" {
		if err := s.discover(); err != nil {
			return nil, err
		}
	}
	audience, ok := s.t.Extra("audience").(string)
	if !ok {
		return nil, fmt.Errorf("token is missing audience parameter")
	}
	newToken, err := oauth.RefreshToken(s.config, s.t, audience)
	if err != nil {
		// TODO if retryable: refresh discovery document and retry once
		// TODO: login required message for expired refresh token
		return nil, fmt.Errorf("error refreshing token: %s", err)
	}

	if err := s.cache.SetToken(s.issuer, s.config.ClientID, newToken); err != nil {
		return nil, err
	}
	return newToken, nil
}
