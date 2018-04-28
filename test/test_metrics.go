package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/flynn/flynn/pkg/httphelper"
	c "github.com/flynn/go-check"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type MetricsSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&MetricsSuite{})

func (s *MetricsSuite) TestSystemMetrics(t *c.C) {
	// boot 3 node cluster
	x := s.bootCluster(t, 3)
	defer x.Destroy()

	// reduce the scrape timeout to 1s
	t.Assert(x.flynn("/", "-a", "metrics", "env", "set", "-t", "prometheus", "SCRAPE_INTERVAL=1s"), Succeeds)

	// create a Prometheus client
	proxy, err := s.clusterProxy(x, "metrics-prometheus.discoverd:80")
	t.Assert(err, c.IsNil)
	defer proxy.Stop()
	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://" + proxy.addr)})
	t.Assert(err, c.IsNil)
	api := v1.NewAPI(client)

	// define functions for checking Prometheus queries
	equal := func(a, b model.Vector) bool {
		if len(a) != len(b) {
			return false
		}
		sort.Sort(a)
		sort.Sort(b)
		for i := range a {
			if !a[i].Metric.Equal(b[i].Metric) {
				return false
			}
			if !a[i].Value.Equal(b[i].Value) {
				return false
			}
		}
		return true
	}
	checkQuery := func(query string, expected model.Vector) {
		var actual model.Vector
		for start := time.Now(); time.Since(start) < 10*time.Second; time.Sleep(time.Second) {
			v, err := api.Query(context.Background(), query, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var ok bool
			actual, ok = v.(model.Vector)
			if !ok {
				t.Fatalf("expected query to return model.Vector, got %T", v)
			}
			if equal(actual, expected) {
				return
			}
		}
		t.Fatalf("timed out waiting for expected samples\nexpected: %v\nactual:   %v", expected, actual)
	}

	// check all jobs are up
	checkQuery("sum(up) by (job)", model.Vector{
		{
			Metric: model.Metric{"job": "flynn-host"},
			Value:  3,
		},
		{
			Metric: model.Metric{"job": "prometheus"},
			Value:  1,
		},
		{
			Metric: model.Metric{"job": "router-api"},
			Value:  3,
		},
	})

	// check there are 3 hosts reporting memory and load metrics
	checkQuery("count(host_memory_total_bytes)", model.Vector{{Value: 3}})
	checkQuery("count(host_memory_avail_bytes)", model.Vector{{Value: 3}})
	checkQuery("count(host_load_1m)", model.Vector{{Value: 3}})
	checkQuery("count(host_load_5m)", model.Vector{{Value: 3}})
	checkQuery("count(host_load_15m)", model.Vector{{Value: 3}})

	// check "router_backend_http_responses_total" matches router responses
	for i := 0; i < 3; i++ {
		res, err := httphelper.RetryClient.Get("http://status." + x.Domain)
		t.Assert(err, c.IsNil)
		res.Body.Close()
		t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	}
	checkQuery(`sum(router_backend_http_responses_total{backend_app="status"}) by (http_status)`, model.Vector{
		{
			Metric: model.Metric{"http_status": "200"},
			Value:  3,
		},
	})
}
