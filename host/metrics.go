package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"

	host "github.com/flynn/flynn/host/types"
	"github.com/inconshreveable/log15"
	"github.com/prometheus/client_golang/prometheus"
)

func registerMetrics(state *State, log log15.Logger) error {
	labels := prometheus.Labels{
		"host_id": state.id,
	}

	return prometheus.Register(&metrics{
		state: state,
		log:   log,
		memTotalDesc: prometheus.NewDesc(
			"host_memory_total_bytes",
			"Host total memory in bytes",
			nil, labels,
		),
		memAvailDesc: prometheus.NewDesc(
			"host_memory_avail_bytes",
			"Host available memory in bytes",
			nil, labels,
		),
		load1mDesc: prometheus.NewDesc(
			"host_load_1m",
			"Host 1m load average",
			nil, labels,
		),
		load5mDesc: prometheus.NewDesc(
			"host_load_5m",
			"Host 5m load average",
			nil, labels,
		),
		load15mDesc: prometheus.NewDesc(
			"host_load_15m",
			"Host 15m load average",
			nil, labels,
		),
		activeJobsDesc: prometheus.NewDesc(
			"host_jobs_active_total",
			"Host active jobs",
			nil, labels,
		),
		startingJobsDesc: prometheus.NewDesc(
			"host_jobs_starting_total",
			"Host starting jobs",
			nil, labels,
		),
		runningJobsDesc: prometheus.NewDesc(
			"host_jobs_running_total",
			"Host running jobs",
			nil, labels,
		),
	})
}

type metrics struct {
	state *State
	log   log15.Logger

	memTotalDesc *prometheus.Desc
	memAvailDesc *prometheus.Desc

	load1mDesc  *prometheus.Desc
	load5mDesc  *prometheus.Desc
	load15mDesc *prometheus.Desc

	activeJobsDesc   *prometheus.Desc
	startingJobsDesc *prometheus.Desc
	runningJobsDesc  *prometheus.Desc
}

func (m *metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.memTotalDesc
	ch <- m.memAvailDesc
	ch <- m.load1mDesc
	ch <- m.load5mDesc
	ch <- m.load15mDesc
	ch <- m.activeJobsDesc
	ch <- m.startingJobsDesc
	ch <- m.runningJobsDesc
}

func (m *metrics) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := m.collectMemoryMetrics(ch); err != nil {
			m.log.Error("error collecting memory metrics", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := m.collectLoadMetrics(ch); err != nil {
			m.log.Error("error collecting load metrics", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := m.collectJobMetrics(ch); err != nil {
			m.log.Error("error collecting job metrics", "err", err)
		}
	}()
	wg.Wait()
}

func (m *metrics) collectMemoryMetrics(ch chan<- prometheus.Metric) error {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			val, err := m.parseMeminfo(line)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(m.memTotalDesc, prometheus.GaugeValue, val)
		case strings.HasPrefix(line, "MemAvailable:"):
			val, err := m.parseMeminfo(line)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(m.memAvailDesc, prometheus.GaugeValue, val)
		}
	}
	return s.Err()
}

func (m *metrics) parseMeminfo(line string) (float64, error) {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid meminfo line: %s", line)
	}
	if parts[2] != "kB" {
		return 0, fmt.Errorf("invalid meminfo units: %s", parts[2])
	}
	val, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing meminfo value: %s", err)
	}
	return val * 1024, nil
}

func (m *metrics) collectLoadMetrics(ch chan<- prometheus.Metric) error {
	data, err := ioutil.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return fmt.Errorf("invalid loadavg data: %s", data)
	}

	// 1m load
	load, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return fmt.Errorf("error parsing 1m loadavg value: %s", err)
	}
	ch <- prometheus.MustNewConstMetric(m.load1mDesc, prometheus.GaugeValue, load)

	// 5m load
	load, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("error parsing 5m loadavg value: %s", err)
	}
	ch <- prometheus.MustNewConstMetric(m.load5mDesc, prometheus.GaugeValue, load)

	// 15m load
	load, err = strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return fmt.Errorf("error parsing 15m loadavg value: %s", err)
	}
	ch <- prometheus.MustNewConstMetric(m.load15mDesc, prometheus.GaugeValue, load)

	return nil
}

func (m *metrics) collectJobMetrics(ch chan<- prometheus.Metric) error {
	jobs := m.state.GetActive()
	var (
		total    float64
		starting float64
		running  float64
	)
	for _, job := range jobs {
		total++
		switch job.Status {
		case host.StatusStarting:
			starting++
		case host.StatusRunning:
			running++
		}
	}
	ch <- prometheus.MustNewConstMetric(m.activeJobsDesc, prometheus.GaugeValue, total)
	ch <- prometheus.MustNewConstMetric(m.startingJobsDesc, prometheus.GaugeValue, starting)
	ch <- prometheus.MustNewConstMetric(m.runningJobsDesc, prometheus.GaugeValue, running)
	return nil
}
