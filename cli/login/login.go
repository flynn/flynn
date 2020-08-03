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

// REAUTH
// - if only one configured issuer, default, otherwise print options and require CLI flag
// - follow login logic to get refresh token for client, exit

// TODO: cluster naming

func Run(args *docopt.Args) error {
	// - if matching refresh token, attempt to get audiences and then jump to cluster selection

	oob := useOOB(args)
	issuer := args.String["<issuer>"]
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

	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   metadata.AuthorizationEndpoint,
			TokenURL:  metadata.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}

	var code *codeInfo
	if oob {
		code, err = loginOOB(cfg)
	} else {
		code, err = loginAuto(cfg)
	}
	if err != nil {
		return err
	}

	// TODO: add audience if controller specified
	t, err := exchangeAuthCode(context.Background(), cfg, code)
	if err != nil {
		return fmt.Errorf("error exchanging code for auth token: %s", err)
	}
	// - if cluster/name specified, skip to ping/add

	cache := config.TokenCache()
	if err := cache.SetToken(issuer, clientID, t); err != nil {
		return fmt.Errorf("error saving initial token: %s", err)
	}

	clusters, err := getClusterList(metadata.AudiencesEndpoint, t)
	if err != nil {
		return fmt.Errorf("error retrieving audiences: %s", err)
	}

	var cluster *cluster
	if len(clusters) == 0 {
		return fmt.Errorf("authentication successful, but user is not authorized for any clusters")
	} else if len(clusters) == 1 {
		cluster = clusters[0]
	} else {
		fmt.Printf("Authentication successful. Available clusters:\n\n")
		for i, c := range clusters {
			fmt.Printf("%d: %s - %s\n", i, c.DisplayName, c.ControllerURL)
		}
		fmt.Printf("Which cluster would you like to add? ")

		var input string
		s := bufio.NewScanner(os.Stdin)
		if s.Scan() {
			input = strings.TrimSpace(s.Text())
		} else {
			return fmt.Errorf("error reading cluster: %s", s.Err())
		}
		fmt.Printf("\n\n")

		var selectedIdx *int
		if i, err := strconv.Atoi(input); err == nil {
			selectedIdx = &i
		}

		if selectedIdx != nil {
			if *selectedIdx > len(clusters)-1 {
				return fmt.Errorf("invalid cluster index %d", selectedIdx)
			}
			cluster = clusters[*selectedIdx]
		}
		if cluster == nil {
			for i, c := range clusters {
				if strings.Contains(fmt.Sprintf("%d: %s - %s\n", i, c.DisplayName, c.ControllerURL), input) {
					cluster = c
					break
				}
			}
			if cluster == nil {
				return fmt.Errorf("unknown cluster %q", input)
			}
		}
	}

	if !strings.HasPrefix(cluster.ControllerURL, "https://controller.") {
		return fmt.Errorf("unexpected controller URL format: %q", cluster.ControllerURL)
	}

	t, err = oauth.RefreshToken(cfg, t, cluster.ControllerURL)
	if err != nil {
		return err
	}
	if err := cache.SetToken(issuer, clientID, t); err != nil {
		return fmt.Errorf("error saving access token: %s", err)
	}

	ts, err := tokensource.New(issuer, cluster.ControllerURL, cache)
	if err != nil {
		return fmt.Errorf("error creating tokensource: %s", err)
	}

	cc, err := controller.NewClientWithHTTP(cluster.ControllerURL, "", oauth2.NewClient(context.Background(), ts))
	if err != nil {
		return fmt.Errorf("error creating controller client: %s", err)
	}
	// check credentials by requesting status endpoint
	_, err = cc.Status()
	if err != nil {
		return fmt.Errorf("error getting controller status: %s", err)
	}

	flynnrc, err := config.ReadFile(config.DefaultPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error reading flynnrc: %s", err)
	}

	domain := strings.TrimPrefix(cluster.ControllerURL, "https://controller.")
	clusterConfig := &config.Cluster{
		Name:          args.String["--cluster-name"],
		OAuthURL:      issuer,
		ControllerURL: cluster.ControllerURL,
		GitURL:        "https://git." + domain,
		ImageURL:      "https://images." + domain,
	}
	if err := flynnrc.Add(clusterConfig, true); err != nil {
		return fmt.Errorf("error saving config: %s", err)
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

	fmt.Printf("\n\n")

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

func exchangeAuthCode(ctx context.Context, config *oauth2.Config, info *codeInfo) (*oauth2.Token, error) {
	t, err := config.Exchange(ctx, info.Code, oauth2.SetAuthURLParam("code_verifier", info.Verifier))
	if err != nil {
		return nil, err
	}
	nonce, ok := t.Extra("nonce").(string)
	if !ok || nonce != info.Nonce {
		return nil, fmt.Errorf("oauth2 auth response has invalid nonce, expected %q, got %q", info.Nonce, nonce)
	}

	return t, nil
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
