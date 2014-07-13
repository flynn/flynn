package exec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
	"github.com/flynn/go-flynn/demultiplex"
)

type Cmd struct {
	Image  string
	HostID string
	JobID  string
	TTY    bool
	Attrs  map[string]string

	Entrypoint []string

	Cmd []string
	Env map[string]string

	Stdin io.Reader

	Stdout io.Writer
	Stderr io.Writer

	TermHeight, TermWidth int

	started      bool
	finished     bool
	cluster      ClusterClient
	closeCluster bool
	attachConn   io.Closer
	errCh        chan error
	streamErr    error

	host cluster.Host
	done chan struct{}

	stderrPipe *readyReader
	stdoutPipe *readyReader
	stdinPipe  *readyWriter
}

func Command(image string, cmd ...string) *Cmd {
	return &Cmd{Image: image, Cmd: cmd, done: make(chan struct{})}
}

type ClusterClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
	DialHost(string) (cluster.Host, error)
}

func CommandUsingCluster(c ClusterClient, image string, cmd ...string) *Cmd {
	command := Command(image, cmd...)
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
	if c.Stdout != nil || c.stdoutPipe != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.started {
		return nil, errors.New("exec: StdoutPipe after job started")
	}
	c.stdoutPipe = newReadyReader()
	c.Stdout = c.stdinPipe
	return c.stdoutPipe, nil
}

func (c *Cmd) StderrPipe() (io.Reader, error) {
	if c.Stderr != nil || c.stderrPipe != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	if c.started {
		return nil, errors.New("exec: StderrPipe after job started")
	}
	c.stderrPipe = newReadyReader()
	return c.stderrPipe, nil
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
		ID: c.JobID,
		Config: &docker.Config{
			Image: c.Image,
			Cmd:   c.Cmd,
			Tty:   c.TTY,
			Env:   formatEnv(c.Env),
		},
		Attributes: c.Attrs,
	}
	if c.Stdout != nil || c.stdoutPipe != nil {
		job.Config.AttachStdout = true
	}
	if c.Stderr != nil || c.stderrPipe != nil {
		job.Config.AttachStderr = true
	}
	if c.Stdin != nil || c.stdinPipe != nil {
		job.Config.AttachStdin = true
		job.Config.OpenStdin = true
		job.Config.StdinOnce = true
	}

	c.host, err = c.cluster.DialHost(c.HostID)
	if err != nil {
		return err
	}

	// subscribe to host events
	ch := make(chan *host.Event)
	stream := c.host.StreamEvents(job.ID, ch)
	go func() {
		for event := range ch {
			if event.Event == "stop" || event.Event == "error" {
				close(c.done)
				return
			}
		}
		c.streamErr = stream.Err()
		close(c.done)
		// TODO: handle disconnections
	}()

	var rwc cluster.ReadWriteCloser
	var attachWait func() error

	if c.Stdout != nil || c.Stderr != nil || c.Stdin != nil ||
		c.stdoutPipe != nil || c.stderrPipe != nil || c.stdinPipe != nil {
		req := &host.AttachReq{
			JobID:  job.ID,
			Height: c.TermHeight,
			Width:  c.TermWidth,
			Flags:  host.AttachFlagStream,
		}
		if job.Config.AttachStdout {
			req.Flags |= host.AttachFlagStdout
		}
		if job.Config.AttachStderr {
			req.Flags |= host.AttachFlagStderr
		}
		if job.Config.AttachStdin {
			req.Flags |= host.AttachFlagStdin
		}
		rwc, attachWait, err = c.host.Attach(req, true)
		if err != nil {
			c.close()
			return err
		}
	}

	goroutines := make([]func() error, 0, 4)

	c.attachConn = rwc
	if attachWait != nil {
		goroutines = append(goroutines, attachWait)
	}

	if c.stdinPipe != nil {
		c.stdinPipe.set(writeCloseCloser{rwc})
	} else if c.Stdin != nil {
		goroutines = append(goroutines, func() error {
			_, err := io.Copy(rwc, c.Stdin)
			rwc.CloseWrite()
			return err
		})
	}
	if !c.TTY {
		if c.stdoutPipe != nil || c.stderrPipe != nil {
			stdout, stderr := demultiplex.Streams(rwc)
			if c.stdoutPipe != nil {
				c.stdoutPipe.set(stdout)
			} else if c.Stdout != nil {
				goroutines = append(goroutines, cpFunc(c.Stdout, stdout))
			}
			if c.stderrPipe != nil {
				c.stderrPipe.set(stderr)
			} else if c.Stderr != nil {
				goroutines = append(goroutines, cpFunc(c.Stderr, stderr))
			}
		} else if c.Stdout != nil || c.Stderr != nil {
			goroutines = append(goroutines, func() error {
				return demultiplex.Copy(c.Stdout, c.Stderr, rwc)
			})
		}
	} else if c.stdoutPipe != nil {
		c.stdoutPipe.set(rwc)
	} else if c.Stdout != nil {
		goroutines = append(goroutines, cpFunc(c.Stdout, rwc))
	}

	c.errCh = make(chan error, len(goroutines))
	for _, fn := range goroutines {
		go func(fn func() error) {
			c.errCh <- fn()
		}(fn)
	}

	_, err = c.cluster.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{c.HostID: {job}}})
	return err
}

func cpFunc(dest io.Writer, src io.Reader) func() error {
	return func() error {
		_, err := io.Copy(dest, src)
		return err
	}
}

func (c *Cmd) close() {
	if c.attachConn != nil {
		c.attachConn.Close()
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
	var err error
	if c.streamErr != nil {
		err = c.streamErr
	}
	job, err := c.host.GetJob(c.JobID)
	if err != nil {
		err = fmt.Errorf("exec: failed to retrieve job state: %s", err)
	}
	if job.Error != nil {
		err = errors.New(*job.Error)
	} else if job.ExitCode != 0 {
		err = ExitError(job.ExitCode)
	}

	for i := 0; i < cap(c.errCh); i++ {
		if copyErr := <-c.errCh; copyErr != nil && err == nil {
			err = copyErr
		}
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
	if c.Stdout != nil || c.stdoutPipe != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = ioutil.Discard
	err := c.Run()
	return b.Bytes(), err
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.Stdout != nil || c.stdoutPipe != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil || c.stderrPipe != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	err := c.Run()
	return b.Bytes(), err
}

type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exec: job exited with status %d", e)
}

func formatEnv(env map[string]string) []string {
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, k+"="+v)
	}
	return res
}

func newReadyReader() *readyReader {
	return &readyReader{ready: make(chan struct{})}
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

type readyReader struct {
	r io.Reader

	ready chan struct{}
}

func (b *readyReader) Read(p []byte) (int, error) {
	<-b.ready
	return b.r.Read(p)
}

func (b *readyReader) set(r io.Reader) {
	b.r = r
	close(b.ready)
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
