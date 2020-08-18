package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	RefreshTokenExpiry    = "refresh_token_expiry"
	RefreshTokenIssueTime = "refresh_token_issue_time"
	issuerMetadataPath    = "/.well-known/oauth-authorization-server"
)

type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e Error) Error() string {
	if e.Description == "" {
		return "oauth error: " + e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Description)
}

type tokenJSON struct {
	AccessToken           string    `json:"access_token"`
	TokenType             string    `json:"token_type"`
	RefreshToken          string    `json:"refresh_token"`
	ExpiresIn             int       `json:"expires_in"`
	RefreshTokenExpiresIn int       `json:"refresh_token_expires_in"`
	RefreshTokenIssueTime time.Time `json:"refresh_token_issue_time"`
}

func RefreshToken(c *oauth2.Config, t *oauth2.Token, audience string) (*oauth2.Token, error) {
	v := make(url.Values)
	v.Set("client_id", c.ClientID)
	v.Set("grant_type", "refresh_token")
	v.Set("refresh_token", t.RefreshToken)
	if audience != "" {
		v.Set("audience", audience)
	}
	req, err := http.NewRequest("POST", c.Endpoint.TokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return nil, &url.Error{
			Op:  "POST",
			URL: c.Endpoint.TokenURL,
			Err: fmt.Errorf("error reading body: %s", err),
		}
	}

	if res.StatusCode != http.StatusOK {
		if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			oauthErr := &Error{}
			if err := json.Unmarshal(body, oauthErr); err == nil && oauthErr.Code != "" {
				return nil, oauthErr
			}
		}
		return nil, &url.Error{
			Op:  "POST",
			URL: c.Endpoint.TokenURL,
			Err: fmt.Errorf("unexpected status %d", res.StatusCode),
		}
	}

	var tj tokenJSON
	if err := json.Unmarshal(body, &tj); err != nil {
		return nil, &url.Error{
			Op:  "POST",
			URL: c.Endpoint.TokenURL,
			Err: fmt.Errorf("error decoding token JSON: %s", err),
		}
	}
	newToken := &oauth2.Token{
		TokenType:    tj.TokenType,
		AccessToken:  tj.AccessToken,
		RefreshToken: tj.RefreshToken,
	}
	if tj.ExpiresIn > 0 {
		newToken.Expiry = time.Now().Add(time.Duration(tj.ExpiresIn) * time.Second)
	}
	raw := make(map[string]interface{})
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &url.Error{
			Op:  "POST",
			URL: c.Endpoint.TokenURL,
			Err: fmt.Errorf("error decoding raw token JSON: %s", err),
		}
	}
	if tj.RefreshTokenExpiresIn > 0 {
		raw[RefreshTokenExpiry] = time.Now().Add(time.Duration(tj.RefreshTokenExpiresIn) * time.Second)
		raw["audience"] = audience
	}
	if !tj.RefreshTokenIssueTime.IsZero() {
		raw[RefreshTokenIssueTime] = tj.RefreshTokenIssueTime
	}

	return newToken.WithExtra(raw), nil
}

type IssuerMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	AudiencesEndpoint     string `json:"audiences_endpoint"`
}

func GetMetadata(u string) (*IssuerMetadata, error) {
	res, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &url.Error{
			Op:  "GET",
			URL: u,
			Err: fmt.Errorf("unexpected status %d", res.StatusCode),
		}
	}

	data := &IssuerMetadata{}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(data); err != nil {
		return nil, &url.Error{
			Op:  "GET",
			URL: u,
			Err: fmt.Errorf("error parsing discovery JSON: %s", err),
		}
	}
	return data, nil
}

func BuildMetadataURL(issuer string) (metadataURL, clientID string, err error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", "", fmt.Errorf("invalid issuer URL: %s", err)
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("invalid issuer URL: scheme must be https")
	}

	if u.Path == "" || u.Path == "/" {
		u.Path = issuerMetadataPath
	} else {
		u.Path = path.Join(issuerMetadataPath, u.Path)
	}

	clientID = u.Query().Get("client_id")
	u.RawQuery = ""

	return u.String(), clientID, nil
}
