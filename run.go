package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"

	"github.com/heroku/hk/term"
)

var (
	detachedRun bool
)

var cmdRun = &Command{
	Run:   runRun,
	Usage: "run command [arguments]",
	Short: "run a job",
	Long:  `Run a job on Flynn`,
}

func init() {
	cmdRun.Flag.BoolVar(&detachedRun, "d", false, "detached")
}

type newJob struct {
	Cmd     []string          `json:"cmd"`
	Env     map[string]string `json:"env"`
	Attach  bool              `json:"attach"`
	TTY     bool              `json:"tty"`
	Columns int               `json:"tty_columns"`
	Lines   int               `json:"tty_lines"`
}

func runRun(cmd *Command, args []string) {
	req := &newJob{
		Cmd:    args,
		TTY:    term.IsTerminal(os.Stdin) && term.IsTerminal(os.Stdout) && !detachedRun,
		Attach: !detachedRun,
	}
	if req.TTY {
		cols, err := term.Cols()
		if err != nil {
			log.Fatal(err)
		}
		lines, err := term.Lines()
		if err != nil {
			log.Fatal(err)
		}
		req.Columns = cols
		req.Lines = lines
		req.Env = map[string]string{
			"COLUMNS": strconv.Itoa(cols),
			"LINES":   strconv.Itoa(lines),
			"TERM":    os.Getenv("TERM"),
		}
	}
	path := "/apps/" + mustApp() + "/jobs"
	if detachedRun {
		must(Post(nil, path, req))
		return
	}

	data, err := json.Marshal(req)
	if err != nil {
		log.Fatal(err)
	}
	httpReq, err := http.NewRequest("POST", path, bytes.NewBuffer(data))
	if err != nil {
		log.Fatal(err)
	}
	c, err := net.Dial("tcp", apiURL[7:])
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	clientconn := httputil.NewClientConn(c, nil)
	res, err := clientconn.Do(httpReq)
	if err != nil {
		log.Fatal(err)
	}
	if res.StatusCode != 200 {
		log.Fatalf("Expected 200, got %d", res.StatusCode)
	}
	conn, bufr := clientconn.Hijack()

	if req.TTY {
		if err := term.MakeRaw(os.Stdin); err != nil {
			log.Fatal(err)
		}
		defer term.Restore(os.Stdin)
	}

	errc := make(chan error)
	go func() {
		buf := make([]byte, bufr.Buffered())
		bufr.Read(buf)
		os.Stdout.Write(buf)
		_, err := io.Copy(os.Stdout, conn)
		errc <- err
	}()
	if _, err := io.Copy(conn, os.Stdin); err != nil {
		log.Fatal(err)
	}
	if err := <-errc; err != nil {
		log.Fatal(err)
	}
}
