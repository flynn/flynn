package cli

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/flynn/flynn/pkg/cluster"
)

var logs = map[string]string{
	"upstart-flynn-host.log": "/var/log/upstart/flynn-host.log",
	"tmp-flynn-host.log":     "/tmp/flynn-host.log",
}

var debugCmds = [][]string{
	{"ps", "faux"},
	{"ifconfig"},
	{"uname", "-a"},
	{"lsb_release", "-a"},
	{"date"},
	{"free", "-m"},
	{"dpkg-query", "-W", "-f", "${Package}: ${Version}\n", "libvirt-bin"},
	{os.Args[0], "version"},
	{"virsh", "-c", "lxc:///", "list"},
	{"virsh", "-c", "lxc:///", "net-list"},
}

func init() {
	Register("upload-debug-info", runUploadDebugInfo, `
usage: flynn-host upload-debug-info

Upload debug information to an anonymous gist`)
}

func runUploadDebugInfo() error {
	gist := &Gist{
		Description: "Flynn debug information",
		Public:      false,
		Files:       make(map[string]File),
	}

	for name, filepath := range logs {
		if err := gist.AddLocalFile(name, filepath); err != nil {
			log.Printf("error adding %s: %s", name, err)
		}
	}

	if err := captureJobs(gist); err != nil {
		log.Println(err)
	}

	var debugOutput string
	for _, cmd := range debugCmds {
		output, err := captureCmd(cmd[0], cmd[1:]...)
		if err != nil {
			log.Printf("could not capture command '%s': %s", strings.Join(cmd, " "), err)
			continue
		}
		debugOutput += fmt.Sprintln("===>", strings.Replace(strings.Join(cmd, " "), "\n", `\n`, -1), "\n", output)
	}
	gist.AddFile("0-debug-output.log", debugOutput)

	if err := gist.Upload(); err != nil {
		return err
	}

	log.Println("Debug information uploaded to:", gist.URL)
	return nil
}

func captureCmd(name string, arg ...string) (string, error) {
	var buf bytes.Buffer
	c := exec.Command(name, arg...)
	c.Stdout = &buf
	if err := c.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func captureJobs(gist *Gist) error {
	client, err := cluster.NewClient()
	if err != nil {
		return err
	}

	jobs, err := jobList(client, true)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	printJobs(jobs, &buf)
	gist.AddFile("1-jobs.log", buf.String())

	for _, job := range jobs {
		var name string
		if app, ok := job.Job.Metadata["flynn-controller.app_name"]; ok {
			name += app + "-"
		}
		if typ, ok := job.Job.Metadata["flynn-controller.type"]; ok {
			name += typ + "-"
		}
		name += job.Job.ID + ".log"

		var content bytes.Buffer
		printJobDesc(&job, &content)
		fmt.Fprint(&content, "\n\n***** ***** ***** ***** ***** ***** ***** ***** ***** *****\n\n")
		getLog(job.HostID, job.Job.ID, client, false, true, &content, &content)

		gist.AddFile(name, content.String())
	}

	return nil
}
