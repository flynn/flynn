package login

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/cli/login/internal/oauth"
	"github.com/flynn/flynn/cli/login/tokensource"
	controller "github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/go-docopt"
	"golang.org/x/oauth2"
)

const (
	oauthHostPort  = "127.0.0.1:8085"
	oobRedirectURI = "urn:ietf:wg:oauth:2.0:oob"
)

func Run(args *docopt.Args) error {
	flynnrc, err := config.ReadFile(config.DefaultPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error reading flynnrc: %s", err)
	}

	existingIssuers := make(map[string][]string)
	existingClusters := make(map[string]*config.Cluster)
	for _, c := range flynnrc.Clusters {
		if c.OAuthURL != "" {
			existingIssuers[c.OAuthURL] = append(existingIssuers[c.OAuthURL], c.Name)
		}
		existingClusters[c.Name] = c
	}

	oob := useOOB(args)
	issuer := args.String["<issuer>"]
	prompt := args.Bool["--prompt"]
	force := args.Bool["--force"]
	clusterName := args.String["--cluster-name"]
	controllerURL := args.String["--controller-url"]

	if issuer == "" && len(existingIssuers) == 1 {
		for k := range existingIssuers {
			issuer = k
		}
	}
	reauth := false
	if !prompt && clusterName == "" && controllerURL == "" && len(existingIssuers[issuer]) > 0 {
		reauth = true
		if len(existingIssuers[issuer]) == 1 {
			clusterName = existingIssuers[issuer][0]
			controllerURL = existingClusters[clusterName].ControllerURL
		}
	}
	if issuer == "" {
		return fmt.Errorf("issuer URL must be specified")
	}
	if !reauth && !prompt && controllerURL == "" {
		return fmt.Errorf("--prompt or --controller-url must be specified to add a new cluster")
	}

	metadataURL, clientID, err := oauth.BuildMetadataURL(issuer)
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("issuer URL is missing client_id parameter")
	}
	metadata, err := oauth.GetMetadata(metadataURL)
	if err != nil {
		return err
	}

	cache := config.TokenCache()
	var clusters []*cluster
	var t *oauth2.Token
	if !reauth {
		t, _ = cache.GetToken(issuer, clientID, "")
		if t != nil {
			clusters, err = getClusterList(metadata.AudiencesEndpoint, t)
			if err != nil {
				t = nil
			}
		}
	}

	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   metadata.AuthorizationEndpoint,
			TokenURL:  metadata.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}

	if t == nil {
		var code *codeInfo
		if oob {
			code, err = loginOOB(cfg)
		} else {
			code, err = loginAuto(cfg)
		}
		if err != nil {
			return err
		}

		t, err = exchangeAuthCode(context.Background(), cfg, code, controllerURL)
		if err != nil {
			return fmt.Errorf("error exchanging code for auth token: %s", err)
		}

		if err := cache.SetToken(issuer, clientID, t); err != nil {
			return fmt.Errorf("error saving initial token: %s", err)
		}
	}

	if reauth {
		return nil
	}

	if prompt {
		if clusters == nil {
			clusters, err = getClusterList(metadata.AudiencesEndpoint, t)
			if err != nil {
				return fmt.Errorf("error retrieving audiences: %s", err)
			}
		}

		s := bufio.NewScanner(os.Stdin)
		if len(clusters) == 0 {
			return fmt.Errorf("authentication successful, but user is not authorized for any clusters")
		} else if len(clusters) == 1 {
			controllerURL = clusters[0].ControllerURL
		} else {
			fmt.Printf("Authentication successful. Available clusters:\n\n")
			for i, c := range clusters {
				fmt.Printf("%d: %s - %s\n", i, c.DisplayName, c.ControllerURL)
			}
			fmt.Printf("Which cluster would you like to add? ")

			var input string
			if s.Scan() {
				input = strings.TrimSpace(s.Text())
			} else {
				return fmt.Errorf("error reading cluster: %s", s.Err())
			}

			var selectedIdx *int
			if i, err := strconv.Atoi(input); err == nil {
				selectedIdx = &i
			}

			if selectedIdx != nil {
				if *selectedIdx > len(clusters)-1 {
					return fmt.Errorf("invalid cluster index %d", selectedIdx)
				}
				controllerURL = clusters[*selectedIdx].ControllerURL
			}
			if controllerURL == "" {
				for i, c := range clusters {
					if strings.Contains(fmt.Sprintf("%d: %s - %s\n", i, c.DisplayName, c.ControllerURL), input) {
						controllerURL = c.ControllerURL
						break
					}
				}
				if controllerURL == "" {
					return fmt.Errorf("unknown cluster %q", input)
				}
			}
		}

		if clusterName == "" {
			fmt.Printf("What should the cluster name be? ")
			if s.Scan() {
				clusterName = strings.TrimSpace(s.Text())
			} else {
				return fmt.Errorf("error reading cluster name: %s", s.Err())
			}
			force = true
		}
	}
	if clusterName == "" {
		clusterName = "default"
	}

	if !strings.HasPrefix(controllerURL, "https://controller.") {
		return fmt.Errorf("unexpected controller URL format: %q", controllerURL)
	}

	t, err = oauth.RefreshToken(cfg, t, controllerURL)
	if err != nil {
		return err
	}
	if err := cache.SetToken(issuer, clientID, t); err != nil {
		return fmt.Errorf("error saving access token: %s", err)
	}

	ts, err := tokensource.New(issuer, controllerURL, cache)
	if err != nil {
		return fmt.Errorf("error creating tokensource: %s", err)
	}

	cc, err := controller.NewClientWithHTTP(controllerURL, "", oauth2.NewClient(context.Background(), ts))
	if err != nil {
		return fmt.Errorf("error creating controller client: %s", err)
	}
	// check credentials by requesting status endpoint
	_, err = cc.Status()
	if err != nil {
		return fmt.Errorf("error getting controller status: %s", err)
	}

	domain := strings.TrimPrefix(controllerURL, "https://controller.")
	clusterConfig := &config.Cluster{
		Name:          clusterName,
		OAuthURL:      issuer,
		ControllerURL: controllerURL,
		GitURL:        "https://git." + domain,
		ImageURL:      "https://images." + domain,
	}
	if err := flynnrc.Add(clusterConfig, force); err != nil {
		return fmt.Errorf("error saving config: %s", err)
	}
	if flynnrc.Default == "" {
		flynnrc.SetDefault(clusterConfig.Name)
	}
	if err := flynnrc.SaveTo(config.DefaultPath()); err != nil {
		return fmt.Errorf("error writing flynnrc: %s", err)
	}

	if _, err := exec.LookPath("git"); err != nil {
		if serr, ok := err.(*exec.Error); ok && serr.Err == exec.ErrNotFound {
			fmt.Println("git was not found. Skipping git configuration.")
		} else {
			return fmt.Errorf("error looking up git path: %s", err)
		}
	} else {
		config.RemoveGlobalGitConfig(clusterConfig.GitURL)
		if err := config.WriteGlobalGitConfig(clusterConfig.GitURL, ""); err != nil {
			return fmt.Errorf("error writing git config: %s", err)
		}
	}

	return nil
}

type cluster struct {
	ControllerURL string
	DisplayName   string
}

func getClusterList(audiencesURL string, t *oauth2.Token) ([]*cluster, error) {
	req, err := http.NewRequest("GET", audiencesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "RefreshToken "+t.RefreshToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &url.Error{
			Op:  "GET",
			URL: audiencesURL,
			Err: fmt.Errorf("unexpected status %d", res.StatusCode),
		}
	}

	var data struct {
		Audiences []struct {
			URL  string
			Name string
			Type string
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, &url.Error{
			Op:  "GET",
			URL: audiencesURL,
			Err: fmt.Errorf("error parsing audiences JSON: %s", err),
		}
	}

	clusters := make([]*cluster, len(data.Audiences))
	for i, a := range data.Audiences {
		if a.Type != "flynn_controller" {
			continue
		}
		clusters[i] = &cluster{
			ControllerURL: a.URL,
			DisplayName:   a.Name,
		}
	}
	return clusters, nil
}

func loginOOB(config *oauth2.Config) (*codeInfo, error) {
	config.RedirectURL = oobRedirectURI
	info := buildAuthCodeURL(config)
	fmt.Printf("To login, open the URL below and then paste the resulting code here:\n  %s\nCode: ", info.URL)

	s := bufio.NewScanner(os.Stdin)
	if s.Scan() {
		info.Code = strings.TrimSpace(s.Text())
	} else {
		return nil, fmt.Errorf("error reading code: %s", s.Err())
	}

	return info, nil
}

func loginAuto(config *oauth2.Config) (*codeInfo, error) {
	config.RedirectURL = "http://" + oauthHostPort + "/"
	info := buildAuthCodeURL(config)

	waitForCode, err := listenForCode(info.State)
	if err != nil {
		fmt.Printf("Error starting automatic code listener: %s\nFalling back to out-of-band code.\n\n", err)
		return loginOOB(config)
	}

	doneCh := make(chan error)
	go func() {
		var err error
		info.Code, err = waitForCode()
		doneCh <- err
	}()

	if err := openURL(info.URL); err != nil {
		fmt.Printf("Unable to open browser, open this URL or re-run this command with --oob-fallback\n  %s\n\n", info.URL)
	} else {
		fmt.Printf("Your browser has been opened to this URL, waiting for authentication to complete...\n  %s\n\n", info.URL)
	}

	return info, <-doneCh
}

func buildAuthCodeURL(config *oauth2.Config) *codeInfo {
	res := &codeInfo{
		Nonce:    random.Base64(32),
		Verifier: random.Base64(32),
	}
	if config.RedirectURL != oobRedirectURI {
		res.State = random.Base64(32)
	}
	challBytes := sha256.Sum256([]byte(res.Verifier))
	res.URL = config.AuthCodeURL(res.State,
		oauth2.SetAuthURLParam("nonce", res.Nonce),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", strings.TrimRight(base64.URLEncoding.EncodeToString(challBytes[:]), "=")),
	)
	return res
}

func exchangeAuthCode(ctx context.Context, config *oauth2.Config, info *codeInfo, audience string) (*oauth2.Token, error) {
	params := []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("code_verifier", info.Verifier)}
	if audience != "" {
		params = append(params, oauth2.SetAuthURLParam("audience", audience))
	}
	t, err := config.Exchange(ctx, info.Code, params...)
	if err != nil {
		return nil, err
	}
	nonce, ok := t.Extra("nonce").(string)
	if !ok || nonce != info.Nonce {
		return nil, fmt.Errorf("oauth2 auth response has invalid nonce, expected %q, got %q", info.Nonce, nonce)
	}

	extra := make(map[string]interface{})
	extra["audience"] = t.Extra("audience")

	iss, _ := t.Extra("refresh_token_issue_time").(string)
	if iss != "" {
		issueTime, err := time.Parse(time.RFC3339Nano, iss)
		if err != nil {
			return nil, fmt.Errorf("error parsing refresh_token_issue_time %q: %s", iss, err)
		}
		extra[oauth.RefreshTokenIssueTime] = issueTime
	}
	exp, ok := t.Extra("refresh_token_expires_in").(float64)
	if ok {
		extra[oauth.RefreshTokenExpiry] = time.Now().Add(time.Duration(exp) * time.Second)
	}

	return t.WithExtra(extra), nil
}

type codeInfo struct {
	URL      string
	Verifier string
	State    string
	Nonce    string
	Code     string
}

func openURL(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = oauthErrFallback
	}
	return err
}

var oauthErrFallback = errors.New("oob fallback")

func useOOB(args *docopt.Args) bool {
	if args.Bool["--oob-code"] {
		return true
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return true
		}
	}
	return false
}

func listenForCode(state string) (func() (string, error), error) {
	l, err := net.Listen("tcp", oauthHostPort)
	if err != nil {
		return nil, err
	}

	return func() (code string, err error) {
		http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer l.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<p>Flynn authentication redirect received, close this page and return to the CLI.</p>"))
			if errCode := r.FormValue("error"); errCode != "" {
				msg := "error from oauth server: " + errCode

				if errDesc := r.FormValue("error_description"); errDesc != "" {
					msg += ": " + errDesc
				}
				if errURI := r.FormValue("error_uri"); errURI != "" {
					msg += " - " + errURI
				}
				err = errors.New(msg)
				return
			}
			if resState := r.FormValue("state"); state != resState {
				err = fmt.Errorf("invalid state in oauth code redirect, wanted %q, got %q", state, resState)
			}
			code = r.FormValue("code")
			if code == "" {
				err = fmt.Errorf("missing code in oauth redirect, got %q", r.URL.RawQuery)
			}
		}))

		return code, err
	}, nil
}
