package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
	"github.com/flynn/flynn/test/cluster/client"
	"github.com/flynn/go-check"
)

var args *arg.Args
var flynnrc string
var routerIP string
var testCluster *testcluster.Client

func init() {
	args = arg.Parse()
	if args.Stream {
		args.Debug = true
	}
	log.SetFlags(log.Lshortfile)
}

func main() {
	defer shutdown.Exit()

	var err error
	var res *check.Result
	// defer exiting here so it runs after all other defers
	defer func() {
		if err != nil || res != nil && !res.Passed() {
			os.Exit(1)
		}
	}()

	flynnrc = args.Flynnrc
	routerIP = args.RouterIP
	if flynnrc == "" {
		c := cluster.New(args.BootConfig, os.Stdout)
		var rootFS string
		rootFS, err = c.BuildFlynn(args.RootFS, "origin/master", false, false)
		if err != nil {
			if args.Kill {
				c.Shutdown()
			}
			log.Println("could not build flynn: ", err)
			if rootFS != "" {
				os.RemoveAll(rootFS)
			}
			return
		}
		if args.BuildRootFS {
			c.Shutdown()
			fmt.Println("Built Flynn in rootfs:", rootFS)
			return
		} else {
			defer os.RemoveAll(rootFS)
		}
		if _, err = c.Boot(cluster.ClusterTypeDefault, 3, nil, args.Kill); err != nil {
			log.Println("could not boot cluster: ", err)
			return
		}
		if args.Kill {
			defer c.Shutdown()
		}

		if err = createFlynnrc(c); err != nil {
			log.Println(err)
			return
		}
		defer os.RemoveAll(flynnrc)

		routerIP = c.RouterIP
	}

	if args.ClusterAPI != "" {
		testCluster, err = testcluster.NewClient(args.ClusterAPI)
		if err != nil {
			log.Println(err)
			return
		}
	}

	if err = setupGitreceive(); err != nil {
		log.Println(err)
		return
	}

	if err = setupDockerPush(); err != nil {
		log.Println(err)
		return
	}

	res = check.RunAll(&check.RunConf{
		Filter:           args.Run,
		Stream:           args.Stream,
		Verbose:          args.Debug,
		KeepWorkDir:      args.Debug,
		ConcurrencyLevel: args.Concurrency,
	})
	fmt.Println(res)
}

func createFlynnrc(c *cluster.Cluster) error {
	tmpfile, err := ioutil.TempFile("", "flynnrc-")
	if err != nil {
		return err
	}
	path := tmpfile.Name()

	config, err := c.CLIConfig()
	if err != nil {
		os.RemoveAll(path)
		return err
	}

	if err := config.SaveTo(path); err != nil {
		os.RemoveAll(path)
		return err
	}

	flynnrc = path
	return nil
}

func setupGitreceive() error {
	// Unencrypted SSH private key for the flynn-test GitHub account.
	// Omits header/footer to avoid any GitHub auto-revoke key crawlers
	sshKey := `MIIEpAIBAAKCAQEA2UnQ/17TfzQRt4HInuP1SYz/tSNaCGO3NDIPLydVu8mmxuKT
zlJtH3pz3uWpMEKdZtSjV+QngJL8OFzanQVZtRBJjF2m+cywHJoZA5KsplMon+R+
QmVqu92WlcRdkcft1F1CLoTXTmHHfvuhOkG6GgJONNLP9Z14EsQ7MbBh5guafWOX
kdGFajyd+T2aj27yIkK44WjWqiLjxRIAtgOJrmd/3H0w3E+O1cgNrA2gkFEUhvR1
OHz8SmugYva0VZWKvxZ6muZvn26L1tajYsCntCRR3/a74cAnVFAXjqSatL6YTbSH
sdtE91kEC73/U4SL3OFdDiCrAvXpJ480C2/GQQIDAQABAoIBAHNQNVYRIPS00WIt
wiZwm8/4wAuFQ1aIdMWCe4Ruv5T1I0kRHZe1Lqwx9CQqhWtTLu1Pk5AlSMF3P9s5
i9sg58arahzP5rlS43OKZBP9Vxq9ryWLwWXDJK2mny/EElQ3YgP9qg29+fVi9thw
+dNM5lK/PnnSFwMmGn77HN712D6Yl3CCJJjsAunTfPzR9hyEqX5YvUB5eq/TNhXe
sqrKcGORIoNfv7WohlFSkTAXIvoMxmFWXg8piZ9/b1W4NwvO4wup3ZSErIk0AQ97
HtyXJIXgtj6pLkPqvPXPGvS3quYAddNxvGIdvge7w5LHnrxOzdqbeDAVmJLVwVlv
oo+7aQECgYEA8ZliUuA8q86SWE0N+JZUqbTvE6VzyWG0/u0BJYDkH7yHkbpFOIEy
KTw048WOZLQ6/wPwL8Hb090Cas/6pmRFMgCedarzXc9fvGEwW95em7jA4AyOVBMC
KIAmaYkm6LcUFeyR6ektZeCkT0MNoi4irjBC3/hMRyZu+6RL4jXxHLkCgYEA5j13
2nkbV99GtRRjyGB7uMkrhMere2MekANXEm4dW+LZFZUda4YCqdzfjDfBTxsuyGqi
DnvI7bZFzIQPiiEzvL2Mpiy7JqxmPLGmwzxDp3z75T5vOrGs4g9IQ7yDjp5WPzjz
KCJJHn8Qt9tNZb5h0hBM+NWLT0c1XxtTIVFfgckCgYAfNpTYZjYQcFDB7bqXWjy3
7DNTE3YhF2l94fra8IsIep/9ONaGlVJ4t1mR780Uv6A7oDOgx+fxuET+rb4RTzUN
X70ZMKvee9M/kELiK5mHftgUWirtO8N0nhHYYqrPOA/1QSoc0U5XMi2oO96ADHvY
i02oh/i63IFMK47OO+/ZqQKBgQCY8bY/Y/nc+o4O1hee0TD+xGvrTXRFh8eSpRVf
QdSw6FWKt76OYbw9OGMr0xHPyd/e9K7obiRAfLeLLyLfgETNGSFodghwnU9g/CYq
RUsv5J+0XjAnTkXo+Xvouz6tK9NhNiSYwYXPA1uItt6IOtriXz+ygLCFHml+3zju
xg5quQKBgQCEL95Di6WD+155gEG2NtqeAOWhgxqAbGjFjfpV+pVBksBCrWOHcBJp
QAvAdwDIZpqRWWMcLS7zSDrzn3ZscuHCMxSOe40HbrVdDUee24/I4YQ+R8EcuzcA
3IV9ai+Bxs6PvklhXmarYxJl62LzPLyv0XFscGRes/2yIIxNfNzFug==`

	out, err := flynnCmd("/", "-a", "gitreceive", "env", "set",
		"SSH_CLIENT_HOSTS=github.com,192.30.252.131 ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
		fmt.Sprintf("SSH_CLIENT_KEY=-----BEGIN RSA PRIVATE KEY-----\n%s\n-----END RSA PRIVATE KEY-----\n", sshKey)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %q", err, string(out))
	}
	return nil
}

func setupDockerPush() error {
	conf, err := config.ReadFile(flynnrc)
	if err != nil {
		return err
	}
	url := strings.Replace(conf.Clusters[0].ControllerURL, "controller", "docker", 1)
	// TODO: remove this `set-push-url' call once CI configures DockerPushURL
	if out, err := flynnCmd("/", "docker", "set-push-url", url).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %q", err, out)
	}
	if out, err := flynnCmd("/", "docker", "login").CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %q", err, out)
	}
	return nil
}

type CmdResult struct {
	Cmd    []string
	Output string
	Err    error
}

func flynnEnv(path string) []string {
	env := os.Environ()
	res := make([]string, 0, len(env)+1)
	for _, v := range env {
		if !strings.HasPrefix(v, "FLYNNRC=") {
			res = append(res, v)
		}
	}
	res = append(res, "FLYNNRC="+path)
	return res
}

func flynnCmd(dir string, cmdArgs ...string) *exec.Cmd {
	cmd := exec.Command(args.CLI, cmdArgs...)
	cmd.Env = flynnEnv(flynnrc)
	cmd.Dir = dir
	return cmd
}

func flynn(t *check.C, dir string, args ...string) *CmdResult {
	return run(t, flynnCmd(dir, args...))
}

func debug(t *check.C, v ...interface{}) {
	t.Log(append([]interface{}{"++ ", time.Now().Format("15:04:05.000"), " "}, v...)...)
}

func debugf(t *check.C, format string, v ...interface{}) {
	t.Logf(strings.Join([]string{"++", time.Now().Format("15:04:05.000"), format}, " "), v...)
}

func run(t *check.C, cmd *exec.Cmd) *CmdResult {
	var out bytes.Buffer
	debug(t, strings.Join(append([]string{cmd.Path}, cmd.Args[1:]...), " "))
	if args.Stream {
		cmd.Stdout = io.MultiWriter(os.Stdout, &out)
		cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	} else {
		cmd.Stdout = &out
		cmd.Stderr = &out
	}
	err := cmd.Run()
	res := &CmdResult{
		Cmd:    cmd.Args,
		Err:    err,
		Output: out.String(),
	}
	if !args.Stream {
		t.Log(res.Output)
	}
	return res
}

var Outputs check.Checker = outputChecker{
	&check.CheckerInfo{
		Name:   "Outputs",
		Params: []string{"result", "output"},
	},
}

type outputChecker struct {
	*check.CheckerInfo
}

func (outputChecker) Check(params []interface{}, names []string) (bool, string) {
	ok, msg, s, res := checkCmdResult(params, names)
	if !ok {
		return ok, msg
	}
	return s == res.Output, ""
}

func checkCmdResult(params []interface{}, names []string) (ok bool, msg, s string, res *CmdResult) {
	res, ok = params[0].(*CmdResult)
	if !ok {
		msg = "result must be a *CmdResult"
		return
	}
	switch v := params[1].(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		msg = "output must be a []byte or string"
		return
	}
	ok = true
	return
}

var OutputContains check.Checker = outputContainsChecker{
	&check.CheckerInfo{
		Name:   "OutputContains",
		Params: []string{"result", "contains"},
	},
}

type outputContainsChecker struct {
	*check.CheckerInfo
}

func (outputContainsChecker) Check(params []interface{}, names []string) (bool, string) {
	ok, msg, s, res := checkCmdResult(params, names)
	if !ok {
		return ok, msg
	}
	return strings.Contains(res.Output, s), ""
}

var Succeeds check.Checker = succeedsChecker{
	&check.CheckerInfo{
		Name:   "Succeeds",
		Params: []string{"result"},
	},
}

type succeedsChecker struct {
	*check.CheckerInfo
}

func (succeedsChecker) Check(params []interface{}, names []string) (bool, string) {
	res, ok := params[0].(*CmdResult)
	if !ok {
		return false, "result must be a *CmdResult"
	}
	return res.Err == nil, ""
}

var SuccessfulOutputContains check.Checker = successfulOutputContainsChecker{
	&check.CheckerInfo{
		Name:   "SuccessfulOutputContains",
		Params: []string{"result", "contains"},
	},
	Succeeds,
	OutputContains,
}

type successfulOutputContainsChecker struct {
	*check.CheckerInfo
	succeeds       check.Checker
	outputContains check.Checker
}

func (s successfulOutputContainsChecker) Check(params []interface{}, names []string) (bool, string) {
	ok, res := s.succeeds.Check(params, names)
	if ok {
		return s.outputContains.Check(params, names)
	}
	return ok, res
}

type matchesChecker struct {
	*check.CheckerInfo
}

var Matches check.Checker = &matchesChecker{
	&check.CheckerInfo{Name: "Matches", Params: []string{"value", "regex"}},
}

func (checker *matchesChecker) Check(params []interface{}, names []string) (result bool, error string) {
	return matches(params[0], params[1])
}

func matches(value, regex interface{}) (result bool, error string) {
	reStr, ok := regex.(string)
	if !ok {
		return false, "Regex must be a string"
	}
	valueStr, valueIsStr := value.(string)
	if !valueIsStr {
		if valueWithStr, valueHasStr := value.(fmt.Stringer); valueHasStr {
			valueStr, valueIsStr = valueWithStr.String(), true
		}
	}
	if valueIsStr {
		matches, err := regexp.MatchString(reStr, valueStr)
		if err != nil {
			return false, "Can't compile regex: " + err.Error()
		}
		return matches, ""
	}
	return false, "Obtained value is not a string and has no .String()"
}
