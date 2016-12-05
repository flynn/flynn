package exec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/schedutil"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/opencontainers/runc/libcontainer/configs"
)

type Cmd struct {
	Job *host.Job

	TTY  bool
	Meta map[string]string

	Args []string

	ImageArtifact *ct.Artifact

	Env map[string]string

	Volumes   []*ct.VolumeReq
	Mounts    []host.Mount
	Resources resource.Resources

	HostNetwork      bool
	HostPIDNamespace bool

	Stdin io.Reader

	Stdout io.Writer
	Stderr io.Writer

	TermHeight, TermWidth uint16

	LinuxCapabilities []string
	AllowedDevices    []*configs.Device

	// cluster is used to communicate with the layer 0 cluster
	cluster ClusterClient

	// Host is used to communicate with the host that the job will run on
	Host *cluster.Host

	// started is true if Start has been called
	started bool

	// finished is true if Wait has been called
	finished bool

	// closeCluster indicates that cluster should be closed after the job
	// finishes, it is set if the cluster connection was created by Start
	closeCluster bool

	// attachClient connects to the job's io streams and is also used to
	// retrieve the job's exit status if any of Stdin, Stdout, or Stderr are
	// specified
	attachClient cluster.AttachClient

	// eventChan is used to get job events (including the exit status) from the
	// host if no io streams are attached
	eventChan chan *host.Event

	// eventStream allows closing eventChan and checking for connection errors,
	// it is only set if eventChan is set
	eventStream stream.Stream

	// streamErr is set if an error is received from attachClient or
	// eventStream, it supercedes a non-zero exitStatus
	streamErr error

	// exitStatus is the job's exit status
	exitStatus int

	// closeAfterWait lists connections that should be closed before Wait returns
	closeAfterWait []io.Closer

	// done is closed after the job exits or fails
	done chan struct{}

	// stdinPipe is set if StdinPipe is called, and holds a readyWriter that
	// blocks until stdin has been attached to the job
	stdinPipe *readyWriter
}

func Command(artifact *ct.Artifact, args ...string) *Cmd {
	return &Cmd{ImageArtifact: artifact, Args: args}
}

func Job(artifact *ct.Artifact, job *host.Job) *Cmd {
	return &Cmd{ImageArtifact: artifact, Job: job}
}

type ClusterClient interface {
	Hosts() ([]*cluster.Host, error)
	Host(string) (*cluster.Host, error)
}

func CommandUsingCluster(c ClusterClient, artifact *ct.Artifact, args ...string) *Cmd {
	command := Command(artifact, args...)
	command.cluster = c
	return command
}

func CommandUsingHost(h *cluster.Host, artifact *ct.Artifact, args ...string) *Cmd {
	command := Command(artifact, args...)
	command.Host = h
	return command
}

func JobUsingCluster(c ClusterClient, artifact *ct.Artifact, job *host.Job) *Cmd {
	command := Job(artifact, job)
	command.cluster = c
	return command
}

func JobUsingHost(h *cluster.Host, artifact *ct.Artifact, job *host.Job) *Cmd {
	command := Job(artifact, job)
	command.Host = h
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
	c.done = make(chan struct{})
	c.started = true
	if c.Host == nil && c.cluster == nil {
		var err error
		c.cluster = cluster.NewClient()
		if err != nil {
			return err
		}
		c.closeCluster = true
	}

	if c.Host == nil {
		hosts, err := c.cluster.Hosts()
		if err != nil {
			return err
		}
		if len(hosts) == 0 {
			return errors.New("exec: no hosts found")
		}
		c.Host = schedutil.PickHost(hosts)
	}

	// Use the pre-defined host.Job configuration if provided;
	// otherwise generate one from the fields on exec.Cmd that mirror stdlib's os.exec.
	if c.Job == nil {
		c.Job = &host.Job{
			Config: host.ContainerConfig{
				Args:             c.Args,
				TTY:              c.TTY,
				Env:              c.Env,
				Stdin:            c.Stdin != nil || c.stdinPipe != nil,
				HostNetwork:      c.HostNetwork,
				HostPIDNamespace: c.HostPIDNamespace,
				Mounts:           c.Mounts,
			},
			Resources: c.Resources,
			Metadata:  c.Meta,
		}
		// if attaching to stdout / stderr, avoid round tripping the
		// streams via on-disk log files.
		if c.Stdout != nil || c.Stderr != nil {
			c.Job.Config.DisableLog = true
		}
	}
	if c.Job.ID == "" {
		c.Job.ID = cluster.GenerateJobID(c.Host.ID(), "")
	}

	if len(c.LinuxCapabilities) > 0 {
		c.Job.Config.LinuxCapabilities = &c.LinuxCapabilities
	}
	if len(c.AllowedDevices) > 0 {
		c.Job.Config.AllowedDevices = &c.AllowedDevices
	}

	for _, vol := range c.Volumes {
		if _, err := utils.ProvisionVolume(vol, c.Host, c.Job); err != nil {
			return err
		}
	}

	resource.SetDefaults(&c.Job.Resources)

	utils.SetupMountspecs(c.Job, []*ct.Artifact{c.ImageArtifact})

	if c.Stdout != nil || c.Stderr != nil || c.Stdin != nil || c.stdinPipe != nil {
		req := &host.AttachReq{
			JobID:  c.Job.ID,
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
		if c.Job.Config.Stdin {
			req.Flags |= host.AttachFlagStdin
		}
		var err error
		c.attachClient, err = c.Host.Attach(req, true)
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

	if c.attachClient == nil {
		c.eventChan = make(chan *host.Event)
		var err error
		c.eventStream, err = c.Host.StreamEvents(c.Job.ID, c.eventChan)
		if err != nil {
			return err
		}
	}

	go func() {
		defer close(c.done)
		if c.attachClient != nil {
			c.exitStatus, c.streamErr = c.attachClient.Receive(c.Stdout, c.Stderr)
		} else {
		outer:
			for e := range c.eventChan {
				switch e.Event {
				case "stop":
					c.exitStatus = *e.Job.ExitStatus
					break outer
				case "error":
					c.streamErr = errors.New(*e.Job.Error)
					break outer
				}
			}
			c.eventStream.Close()
			if c.streamErr == nil {
				c.streamErr = c.eventStream.Err()
			}
		}
	}()

	return c.Host.AddJob(c.Job)
}

func (c *Cmd) close() {
	if c.attachClient != nil {
		c.attachClient.Close()
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
	return c.Host.StopJob(c.Job.ID)
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
	return c.attachClient.Signal(sig)
}

func (c *Cmd) ResizeTTY(height, width uint16) error {
	if !c.started {
		return errors.New("exec: not started")
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
