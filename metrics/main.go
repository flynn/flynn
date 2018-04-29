package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/inconshreveable/log15"
)

const (
	defaultScrapeInterval = "10s"
)

var log = log15.New("app", "metrics")

type job struct {
	Service    string
	ConfigPath string
}

func main() {
	config := &globalConfigData{
		ScrapeInterval: defaultScrapeInterval,
		Jobs: []*job{
			{"flynn-host", "/etc/prometheus/flynn-host.json"},
			{"router-api", "/etc/prometheus/router-api.json"},
		},
	}

	if v := os.Getenv("SCRAPE_INTERVAL"); v != "" {
		config.ScrapeInterval = v
	}

	ctx, cancel := context.WithCancel(context.Background())
	shutdown.BeforeExit(cancel)

	if err := run(ctx, config); err != nil {
		shutdown.Fatalf("error running metrics backend: %s", err)
	}
}

func run(ctx context.Context, config *globalConfigData) error {
	if err := writeGlobalConfig(config); err != nil {
		return err
	}

	if err := configureJobs(ctx, config.Jobs); err != nil {
		return err
	}

	return runPrometheus(ctx)
}

const globalConfigPath = "/etc/prometheus.yml"

type globalConfigData struct {
	ScrapeInterval string
	Jobs           []*job
}

var globalConfig = template.Must(template.New("config").Parse(`
global:
  scrape_interval: {{ .ScrapeInterval }}

remote_write:
    - url: "http://metrics-prometheus-postgresql.discoverd/write"
remote_read:
    - url: "http://metrics-prometheus-postgresql.discoverd/read"

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ['localhost:80']

{{ range .Jobs }}
  - job_name: {{ .Service }}
    file_sd_configs:
      - files:
        - {{ .ConfigPath }}
{{ end }}
`))

func writeGlobalConfig(config *globalConfigData) error {
	log.Info("writing global config file", "path", globalConfigPath)
	if err := os.MkdirAll(filepath.Dir(globalConfigPath), 0755); err != nil {
		return fmt.Errorf("error creating global config directory: %s", err)
	}
	configFile, err := os.OpenFile(globalConfigPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error opening global config file: %s", err)
	}
	defer configFile.Close()
	if err := globalConfig.Execute(configFile, config); err != nil {
		return fmt.Errorf("error writing global config file: %s", err)
	}
	return nil
}

func configureJobs(ctx context.Context, jobs []*job) error {
	for _, job := range jobs {
		log.Info("configuring job", "service", job.Service, "configPath", job.ConfigPath)
		if err := configureJob(ctx, job); err != nil {
			log.Error("error configuring job", "service", job.Service, "err", err)
			return err
		}
	}
	return nil
}

func configureJob(ctx context.Context, job *job) error {
	svc := discoverd.NewService(job.Service)
	events := make(chan *discoverd.Event)
	stream, err := svc.Watch(events)
	if err != nil {
		return err
	}
	firstErr := make(chan error)
	go func() {
		defer stream.Close()
		current := false
		instances := make(map[string]*discoverd.Instance)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					log.Error("error watching service", "service", job.Service, "err", stream.Err())
					if !current {
						firstErr <- stream.Err()
					}
					return
				}
				switch event.Kind {
				case discoverd.EventKindCurrent:
					if !current {
						current = true
						firstErr <- writeJobConfig(job, instances)
					}
				case discoverd.EventKindUp:
					instances[event.Instance.Addr] = event.Instance
					if err := writeJobConfig(job, instances); err != nil {
						log.Error("error writing config", "service", job.Service, "err", err)
					}
				case discoverd.EventKindDown:
					delete(instances, event.Instance.Addr)
					if err := writeJobConfig(job, instances); err != nil {
						log.Error("error writing config", "service", job.Service, "err", err)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	select {
	case err := <-firstErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type jobConfig struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func writeJobConfig(job *job, instances map[string]*discoverd.Instance) error {
	config := make([]*jobConfig, 0, len(instances))
	for _, inst := range instances {
		labels := make(map[string]string)
		if jobID, ok := inst.Meta["FLYNN_JOB_ID"]; ok {
			if hostID, err := cluster.ExtractHostID(jobID); err == nil {
				labels["host_id"] = hostID
			}
			labels["job_id"] = jobID
		}
		config = append(config, &jobConfig{
			Targets: []string{inst.Addr},
			Labels:  labels,
		})
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(job.ConfigPath), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(job.ConfigPath, data, 0644)
}

func runPrometheus(ctx context.Context) error {
	cmd := exec.Command(
		"/var/lib/prometheus/prometheus",
		"--config.file", globalConfigPath,
		"--web.listen-address", net.JoinHostPort("0.0.0.0", os.Getenv("PORT")),
		"--storage.tsdb.path", "/data",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Info("starting prometheus")
	if err := cmd.Start(); err != nil {
		log.Error("error starting prometheus", "err", err)
		return err
	}
	go func() {
		<-ctx.Done()
		log.Info("stopping prometheus")
		cmd.Process.Signal(syscall.SIGTERM)
	}()
	return cmd.Wait()
}
