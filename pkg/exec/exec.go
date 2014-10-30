package exec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type Cmd struct {
	HostID string
	JobID  string
	TTY    bool
	Meta   map[string]string

	Entrypoint []string

	Artifact host.Artifact

	Cmd []string
	Env map[string]string

	Stdin io.Reader

	Stdout io.Writer
	Stderr io.Writer

	TermHeight, TermWidth uint16

	started      bool
	finished     bool
	cluster      ClusterClient
	closeCluster bool
	attachClient cluster.AttachClient
	streamErr    error
	exitStatus   int

	closeAfterWait []io.Closer

	host cluster.Host
	done chan struct{}

	stdinPipe *readyWriter
}

func DockerImage(uri string) host.Artifact {
	return host.Artifact{Type: "docker", URI: uri}
}

func Command(artifact host.Artifact, cmd ...string) *Cmd {
	return &Cmd{Artifact: artifact, Cmd: cmd, done: make(chan struct{})}
}

type ClusterClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
	DialHost(string) (cluster.Host, error)
}

func CommandUsingCluster(c ClusterClient, artifact host.Artifact, cmd ...string) *Cmd {
	command := Command(artifact, cmd...)
	command.cluster = c
	return command
}

func (c *Cmd) StdinPipe() (io.WriteCloser, error) {
	if c.Stdin != nil || c.stdinPipe != nil {
		return nil, errors.New("exec: Stdin already set")
	}
	if c.started {
		return nil, errors.New("exec: StdinPipe after job started")
	}
	c.stdinPipe = newReadyWriter()
	return c.stdinPipe, nil
}

func (c *Cmd) StdoutPipe() (io.Reader, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.started {
		return nil, errors.New("exec: StdoutPipe after job started")
	}
	r, w := io.Pipe()
	c.Stdout = w
	c.closeAfterWait = append(c.closeAfterWait, w)
	return r, nil
}

func (c *Cmd) StderrPipe() (io.Reader, error) {
	if c.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	if c.started {
		return nil, errors.New("exec: StderrPipe after job started")
	}
	r, w := io.Pipe()
	c.Stderr = w
	c.closeAfterWait = append(c.closeAfterWait, w)
	return r, nil
}

func (c *Cmd) Start() error {
	if c.started {
		return errors.New("exec: already started")
	}
	c.started = true
	if c.cluster == nil {
		var err error
		c.cluster, err = cluster.NewClient()
		if err != nil {
			return err
		}
		c.closeCluster = true
	}

	hosts, err := c.cluster.ListHosts()
	if err != nil {
		return err
	}
	if c.HostID == "" {
		// TODO: check if this is actually random
		for c.HostID = range hosts {
			break
		}
	}
	if c.JobID == "" {
		c.JobID = cluster.RandomJobID("")
	}

	job := &host.Job{
		ID:       c.JobID,
		Artifact: c.Artifact,
		Config: host.ContainerConfig{
			Entrypoint: c.Entrypoint,
			Cmd:        c.Cmd,
			TTY:        c.TTY,
			Env:        c.Env,
			Stdin:      c.Stdin != nil || c.stdinPipe != nil,
		},
		Metadata: c.Meta,
	}

	c.host, err = c.cluster.DialHost(c.HostID)
	if err != nil {
		return err
	}

	if c.Stdout != nil || c.Stderr != nil || c.Stdin != nil || c.stdinPipe != nil {
		req := &host.AttachReq{
			JobID:  job.ID,
			Height: c.TermHeight,
			Width:  c.TermWidth,
			Flags:  host.AttachFlagStream,
		}
		if c.Stdout != nil {
			req.Flags |= host.AttachFlagStdout
		}
		if c.Stderr != nil {
			req.Flags |= host.AttachFlagStderr
		}
		if job.Config.Stdin {
			req.Flags |= host.AttachFlagStdin
		}
		c.attachClient, err = c.host.Attach(req, true)
		if err != nil {
			c.close()
			return err
		}
	}

	if c.stdinPipe != nil {
		c.stdinPipe.set(writeCloseCloser{c.attachClient})
	} else if c.Stdin != nil {
		go func() {
			io.Copy(c.attachClient, c.Stdin)
			c.attachClient.CloseWrite()
		}()
	}
	go func() {
		c.exitStatus, c.streamErr = c.attachClient.Receive(c.Stdout, c.Stderr)
		close(c.done)
	}()

	_, err = c.cluster.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{c.HostID: {job}}})
	return err
}

func (c *Cmd) close() {
	if c.attachClient != nil {
		c.attachClient.Close()
	}
	if c.host != nil {
		c.host.Close()
	}
	if c.closeCluster {
		c.cluster.(*cluster.Client).Close()
	}
}

func (c *Cmd) Wait() error {
	if !c.started {
		return errors.New("exec: not started")
	}
	if c.finished {
		return errors.New("exec: Wait was already called")
	}
	c.finished = true

	<-c.done

	for _, wc := range c.closeAfterWait {
		wc.Close()
	}

	var err error
	if c.exitStatus != 0 {
		err = ExitError(c.exitStatus)
	} else if c.streamErr != nil {
		err = c.streamErr
	}

	c.close()

	return err
}

func (c *Cmd) Kill() error {
	if !c.started {
		return errors.New("exec: not started")
	}
	return c.host.StopJob(c.JobID)
}

func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *Cmd) Output() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = ioutil.Discard
	err := c.Run()
	return b.Bytes(), err
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	err := c.Run()
	return b.Bytes(), err
}

func (c *Cmd) Signal(sig int) error {
	if !c.started {
		return errors.New("exec: not started")
	}
	if c.finished {
		return errors.New("exec: already finished")
	}
	return c.attachClient.Signal(sig)
}

func (c *Cmd) ResizeTTY(height, width uint16) error {
	if !c.started {
		return errors.New("exec: not started")
	}
	if c.finished {
		return errors.New("exec: already finished")
	}
	return c.attachClient.ResizeTTY(height, width)
}

type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exec: job exited with status %d", e)
}

type writeCloser interface {
	Write([]byte) (int, error)
	CloseWrite() error
}

type writeCloseCloser struct {
	writeCloser
}

func (c writeCloseCloser) Close() error {
	return c.CloseWrite()
}

func newReadyWriter() *readyWriter {
	return &readyWriter{ready: make(chan struct{})}
}

type readyWriter struct {
	w io.WriteCloser

	ready chan struct{}
}

func (b *readyWriter) Write(p []byte) (int, error) {
	<-b.ready
	return b.w.Write(p)
}

func (b *readyWriter) set(w io.WriteCloser) {
	b.w = w
	close(b.ready)
}

func (b *readyWriter) Close() error {
	<-b.ready
	return b.w.Close()
}
