package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"syscall"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/schedutil"
)

type HostSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&HostSuite{})

func (s *HostSuite) TestAttachNonExistentJob(t *c.C) {
	cluster := s.clusterClient(t)
	hosts, err := cluster.ListHosts()
	t.Assert(err, c.IsNil)

	h := s.hostClient(t, hosts[0].ID)

	// Attaching to a non-existent job should error
	_, err = h.Attach(&host.AttachReq{JobID: "none", Flags: host.AttachFlagLogs}, false)
	t.Assert(err, c.NotNil)
}

func (s *HostSuite) TestAttachFinishedInteractiveJob(t *c.C) {
	cluster := s.clusterClient(t)

	// run a quick interactive job
	cmd := exec.CommandUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), "/bin/true")
	cmd.TTY = true
	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for interactive job")
	}

	h, err := cluster.DialHost(cmd.HostID)
	t.Assert(err, c.IsNil)

	// Getting the logs for the job should fail, as it has none because it was
	// interactive
	attachErr := make(chan error)
	go func() {
		_, err = h.Attach(&host.AttachReq{JobID: cmd.Job.ID, Flags: host.AttachFlagLogs}, false)
		attachErr <- err
	}()
	select {
	case err := <-attachErr:
		t.Assert(err, c.NotNil)
	case <-time.After(time.Second):
		t.Error("timed out waiting for attach")
	}
}

func (s *HostSuite) TestExecCrashingJob(t *c.C) {
	cluster := s.clusterClient(t)

	for _, attach := range []bool{true, false} {
		t.Logf("attach = %v", attach)
		cmd := exec.CommandUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), "sh", "-c", "exit 1")
		if attach {
			cmd.Stdout = ioutil.Discard
			cmd.Stderr = ioutil.Discard
		}
		t.Assert(cmd.Run(), c.DeepEquals, exec.ExitError(1))
	}
}

/*
	Make an 'ish' application on the given host, returning it when
	it has registered readiness with discoverd.

	User will want to defer cmd.Kill() to clean up.
*/
func makeIshApp(cluster *cluster.Client, h cluster.Host, dc *discoverd.Client, extraConfig host.ContainerConfig) (*exec.Cmd, *discoverd.Instance, error) {
	// pick a unique string to use as service name so this works with concurrent tests.
	serviceName := "ish-service-" + random.String(6)

	// run a job that accepts tcp connections and performs tasks we ask of it in its container
	cmd := exec.JobUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), &host.Job{
		Config: host.ContainerConfig{
			Cmd:   []string{"/bin/ish"},
			Ports: []host.Port{{Proto: "tcp"}},
			Env: map[string]string{
				"NAME": serviceName,
			},
		}.Merge(extraConfig),
	})
	cmd.HostID = h.ID()
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	// wait for the job to heartbeat and return its address
	services, err := dc.Instances(serviceName, time.Second*100)
	if err != nil {
		cmd.Kill()
		return nil, nil, err
	}
	if len(services) != 1 {
		cmd.Kill()
		return nil, nil, fmt.Errorf("test setup: expected exactly one service instance, got %d", len(services))
	}

	return cmd, services[0], nil
}

func runIshCommand(service *discoverd.Instance, cmd string) (string, error) {
	resp, err := http.Post(
		fmt.Sprintf("http://%s/ish", service.Addr),
		"text/plain",
		strings.NewReader(cmd),
	)
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *HostSuite) TestNetworkedPersistentJob(t *c.C) {
	// this isn't much more impressive than what's already running by the time we've got a cluster engaged
	// but the idea is to use this basic design to enable testing a series manipulations on a single container.

	cluster := s.clusterClient(t)

	// run a job that accepts tcp connections and performs tasks we ask of it in its container
	cmd, service, err := makeIshApp(cluster, s.anyHostClient(t), s.discoverdClient(t), host.ContainerConfig{})
	t.Assert(err, c.IsNil)
	defer cmd.Kill()

	// test that we can interact with that job
	resp, err := runIshCommand(service, "echo echocococo")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "echocococo\n")
}

func (s *HostSuite) TestVolumeCreation(t *c.C) {
	h := s.anyHostClient(t)

	vol, err := h.CreateVolume("default")
	t.Assert(err, c.IsNil)
	t.Assert(vol.ID, c.Not(c.Equals), "")
	t.Assert(h.DestroyVolume(vol.ID), c.IsNil)
}

func (s *HostSuite) TestVolumeCreationFailsForNonexistentProvider(t *c.C) {
	h := s.anyHostClient(t)

	_, err := h.CreateVolume("non-existent")
	t.Assert(err, c.NotNil)
}

func (s *HostSuite) TestVolumePersistence(t *c.C) {
	t.Skip("test intermittently fails due to host bind mount leaks, see https://github.com/flynn/flynn/issues/1125")

	// most of the volume tests (snapshotting, quotas, etc) are unit tests under their own package.
	// these tests exist to cover the last mile where volumes are bind-mounted into containers.

	cluster := s.clusterClient(t)
	h := s.anyHostClient(t)

	// create a volume!
	vol, err := h.CreateVolume("default")
	t.Assert(err, c.IsNil)
	defer func() {
		t.Assert(h.DestroyVolume(vol.ID), c.IsNil)
	}()

	// create first job
	cmd, service, err := makeIshApp(cluster, h, s.discoverdClient(t), host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: true,
		}},
	})
	t.Assert(err, c.IsNil)
	defer cmd.Kill()
	// add data to the volume
	resp, err := runIshCommand(service, "echo 'testcontent' > /vol/alpha ; echo $?")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "0\n")

	// start another one that mounts the same volume
	cmd, service, err = makeIshApp(cluster, h, s.discoverdClient(t), host.ContainerConfig{
		Volumes: []host.VolumeBinding{{
			Target:    "/vol",
			VolumeID:  vol.ID,
			Writeable: false,
		}},
	})
	t.Assert(err, c.IsNil)
	defer cmd.Kill()
	// read data back from the volume
	resp, err = runIshCommand(service, "cat /vol/alpha")
	t.Assert(err, c.IsNil)
	t.Assert(resp, c.Equals, "testcontent\n")
}

func (s *HostSuite) TestSignalJob(t *c.C) {
	cluster := s.clusterClient(t)

	// pick a host to run the job on
	hosts, err := cluster.ListHosts()
	t.Assert(err, c.IsNil)
	hostID := schedutil.PickHost(hosts).ID

	// start a signal-service job
	cmd := exec.JobUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), &host.Job{
		Config: host.ContainerConfig{Cmd: []string{"/bin/signal"}},
	})
	cmd.HostID = hostID
	var out bytes.Buffer
	cmd.Stdout = &out
	t.Assert(cmd.Start(), c.IsNil)
	_, err = s.discoverdClient(t).Instances("signal-service", 10*time.Second)
	t.Assert(err, c.IsNil)

	// send the job a signal
	client, err := cluster.DialHost(hostID)
	t.Assert(err, c.IsNil)
	t.Assert(client.SignalJob(cmd.Job.ID, int(syscall.SIGTERM)), c.IsNil)

	// wait for the job to exit
	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		t.Assert(err, c.IsNil)
	case <-time.After(12 * time.Second):
		t.Fatal("timed out waiting for job to stop")
	}

	// check the output
	t.Assert(out.String(), c.Equals, "got signal: terminated")
}

func (s *HostSuite) TestResourceLimits(t *c.C) {
	cmd := exec.JobUsingCluster(
		s.clusterClient(t),
		exec.DockerImage(imageURIs["test-apps"]),
		&host.Job{
			Config:    host.ContainerConfig{Cmd: []string{"sh", "-c", resourceCmd}},
			Resources: testResources(),
		},
	)
	var out bytes.Buffer
	cmd.Stdout = &out

	runErr := make(chan error)
	go func() {
		runErr <- cmd.Run()
	}()
	select {
	case err := <-runErr:
		t.Assert(err, c.IsNil)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for resource limits job")
	}

	assertResourceLimits(t, out.String())
}
