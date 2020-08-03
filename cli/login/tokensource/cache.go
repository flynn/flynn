package tokensource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/flynn/flynn/cli/login/internal/oauth"
	"github.com/flynn/flynn/pkg/lockedfile"
	"golang.org/x/oauth2"
)

type Cache interface {
	GetToken(issuer, clientID, audience string) (*oauth2.Token, error)
	SetToken(issuer, clientID string, t *oauth2.Token) error
}

func NewTokenCache(dir string) Cache {
	return &cache{baseDir: dir}
}

type cache struct {
	baseDir string
}

type tokenCache struct {
	RefreshToken       string    `json:"refresh_token"`
	RefreshTokenExpiry time.Time `json:"refresh_token_expiry"`

	AccessTokens map[string]*accessToken `json:"access_tokens"`
}

type accessToken struct {
	AccessToken string    `json:"access_token"`
	Expiry      time.Time `json:"expiry"`
	TokenType   string    `json:"token_type"`
}

func (c *cache) filepath(issuer, clientID string) (string, string, error) {
	issuerURL, err := url.Parse(issuer)
	if err != nil {
		return "", "", fmt.Errorf("invalid issuer URL: %s", err)
	}
	return filepath.Join(c.baseDir, issuerURL.Host), clientID + ".json", nil
}

var ErrTokenNotFound = errors.New("cached token not found")

func (c *cache) readCache(path string) (*tokenCache, error) {
	data, err := lockedfile.Read(path)
	if err == os.ErrNotExist {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error reading token cache %q: %s", path, err)
	}
	res := &tokenCache{}
	if err := json.Unmarshal(data, res); err != nil {
		return nil, fmt.Errorf("error decoding token cache %q: %s", path, err)
	}
	return res, nil
}

func (c *cache) GetToken(issuer, clientID, audience string) (*oauth2.Token, error) {
	dir, filename, err := c.filepath(issuer, clientID)
	if err != nil {
		return nil, err
	}
	cache, err := c.readCache(filepath.Join(dir, filename))
	if err != nil {
		return nil, err
	}

	if cache.RefreshToken == "" {
		return nil, ErrTokenNotFound
	}

	t := &oauth2.Token{
		RefreshToken: cache.RefreshToken,
	}
	if audience == "" {
		t.AccessToken = cache.RefreshToken
		t.TokenType = "RefreshToken"
		t.Expiry = cache.RefreshTokenExpiry
	} else {
		at, ok := cache.AccessTokens[audience]
		if !ok || at.AccessToken == "" {
			return nil, ErrTokenNotFound
		}
		t.AccessToken = at.AccessToken
		t.TokenType = at.TokenType
		t.Expiry = at.Expiry
	}
	t = t.WithExtra(map[string]interface{}{
		oauth.RefreshTokenExpiry: cache.RefreshTokenExpiry,
		"audience":               audience,
	})

	return t, nil
}

func (c *cache) SetToken(issuer, clientID string, t *oauth2.Token) error {
	dir, filename, err := c.filepath(issuer, clientID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creating token cache directory %q: %s", dir, err)
	}

	cacheFilePath := filepath.Join(dir, filename)
	return lockedfile.Transform(cacheFilePath, 0600, func(old []byte) ([]byte, error) {
		cached := &tokenCache{}
		if len(old) > 0 {
			if err := json.Unmarshal(old, cached); err != nil {
				return nil, fmt.Errorf("error decoding existing token cache %q: %s", cacheFilePath, err)
			}
		}
		if cached.AccessTokens == nil {
			cached.AccessTokens = make(map[string]*accessToken)
		}

		refreshExpiry, ok := t.Extra(oauth.RefreshTokenExpiry).(time.Time)
		if !ok || refreshExpiry.After(cached.RefreshTokenExpiry) || cached.RefreshToken == "" {
			cached.RefreshToken = t.RefreshToken
			cached.RefreshTokenExpiry = refreshExpiry
		}
		if audience, ok := t.Extra("audience").(string); ok && audience != "" {
			if oldToken, ok := cached.AccessTokens[audience]; !ok || t.Expiry.After(oldToken.Expiry) || t.AccessToken == "" {
				cached.AccessTokens[audience] = &accessToken{
					AccessToken: t.AccessToken,
					TokenType:   t.TokenType,
					Expiry:      t.Expiry,
				}
			}
		}

		return json.MarshalIndent(cached, "", "\t")
	})
}
