package pgprometheus

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
)

// Config for the database
type Config struct {
	host                      string
	port                      int
	user                      string
	password                  string
	database                  string
	schema                    string
	sslMode                   string
	table                     string
	copyTable                 string
	maxOpenConns              int
	maxIdleConns              int
	pgPrometheusNormalize     bool
	pgPrometheusLogSamples    bool
	pgPrometheusChunkInterval time.Duration
	useTimescaleDb            bool
}

// ParseFlags parses the configuration flags specific to PostgreSQL and TimescaleDB
func ParseFlags(cfg *Config) *Config {
	flag.StringVar(&cfg.host, "pg.host", "localhost", "The PostgreSQL host")
	flag.IntVar(&cfg.port, "pg.port", 5432, "The PostgreSQL port")
	flag.StringVar(&cfg.user, "pg.user", "postgres", "The PostgreSQL user")
	flag.StringVar(&cfg.password, "pg.password", "", "The PostgreSQL password")
	flag.StringVar(&cfg.database, "pg.database", "postgres", "The PostgreSQL database")
	flag.StringVar(&cfg.schema, "pg.schema", "", "The PostgreSQL schema")
	flag.StringVar(&cfg.sslMode, "pg.ssl-mode", "disable", "The PostgreSQL connection ssl mode")
	flag.StringVar(&cfg.table, "pg.table", "metrics", "The PostgreSQL table")
	flag.StringVar(&cfg.copyTable, "pg.copy-table", "metrics_copy", "The PostgreSQL table")
	flag.IntVar(&cfg.maxOpenConns, "pg.max-open-conns", 50, "The max number of open connections to the database")
	flag.IntVar(&cfg.maxIdleConns, "pg.max-idle-conns", 10, "The max number of idle connections to the database")
	flag.BoolVar(&cfg.pgPrometheusNormalize, "pg.prometheus-normalized-schema", true, "Insert metric samples into normalized schema")
	flag.BoolVar(&cfg.pgPrometheusLogSamples, "pg.prometheus-log-samples", false, "Log raw samples to stdout")
	flag.DurationVar(&cfg.pgPrometheusChunkInterval, "pg.prometheus-chunk-interval", time.Hour*12, "The size of a time-partition chunk in TimescaleDB")
	flag.BoolVar(&cfg.useTimescaleDb, "pg.use-timescaledb", true, "Use timescaleDB")
	return cfg
}

// Client sends Prometheus samples to PostgreSQL
type Client struct {
	logger log.Logger
	db     *sql.DB
	cfg    *Config
}

// NewClient creates a new PostgreSQL client
func NewClient(logger log.Logger, cfg *Config) *Client {
	connStr := fmt.Sprintf("host=%v port=%v user=%v dbname=%v password='%v' sslmode=%v connect_timeout=10",
		cfg.host, cfg.port, cfg.user, cfg.database, cfg.password, cfg.sslMode)

	if logger == nil {
		logger = log.NewNopLogger()
	}

	db, err := sql.Open("postgres", connStr)

	level.Info(logger).Log("msg", connStr)

	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}

	db.SetMaxOpenConns(cfg.maxOpenConns)
	db.SetMaxIdleConns(cfg.maxIdleConns)

	client := &Client{
		logger: log.With(logger, "storage", "PostgreSQL"),
		db:     db,
		cfg:    cfg,
	}

	err = client.setupPgPrometheus()

	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}

	return client
}

func (c *Client) setupPgPrometheus() error {
	tx, err := c.db.Begin()

	if err != nil {
		return err
	}

	defer tx.Rollback()

	_, err = tx.Exec("CREATE EXTENSION IF NOT EXISTS pg_prometheus")

	if err != nil {
		return err
	}

	if c.cfg.useTimescaleDb {
		_, err = tx.Exec("CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE")
	}
	if err != nil {
		level.Info(c.logger).Log("msg", "Could not enable TimescaleDB extension", "err", err)
	}

	var rows *sql.Rows
	rows, err = tx.Query("SELECT create_prometheus_table($1, normalized_tables => $2, chunk_time_interval => $3,  use_timescaledb=> $4)",
		c.cfg.table, c.cfg.pgPrometheusNormalize, c.cfg.pgPrometheusChunkInterval.String(), c.cfg.useTimescaleDb)

	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	}
	rows.Close()

	err = tx.Commit()

	if err != nil {
		return err
	}

	level.Info(c.logger).Log("msg", "Initialized pg_prometheus extension")

	return nil
}

func metricString(m model.Metric) string {
	metricName, hasName := m[model.MetricNameLabel]
	numLabels := len(m) - 1
	if !hasName {
		numLabels = len(m)
	}
	labelStrings := make([]string, 0, numLabels)
	for label, value := range m {
		if label != model.MetricNameLabel {
			labelStrings = append(labelStrings, fmt.Sprintf("%s=%q", label, value))
		}
	}

	switch numLabels {
	case 0:
		if hasName {
			return string(metricName)
		}
		return "{}"
	default:
		sort.Strings(labelStrings)
		return fmt.Sprintf("%s{%s}", metricName, strings.Join(labelStrings, ","))
	}
}

// Write implements the Writer interface and writes metric samples to the database
func (c *Client) Write(samples model.Samples) error {
	begin := time.Now()
	tx, err := c.db.Begin()

	if err != nil {
		level.Error(c.logger).Log("msg", "Error on Begin when writing samples", "err", err)
		return err
	}

	defer tx.Rollback()

	stmt, err := tx.Prepare(fmt.Sprintf("COPY \"%s\" FROM STDIN", c.cfg.copyTable))

	if err != nil {
		level.Error(c.logger).Log("msg", "Error on Prepare when writing samples", "err", err)
		return err
	}

	for _, sample := range samples {
		milliseconds := sample.Timestamp.UnixNano() / 1000000
		line := fmt.Sprintf("%v %v %v", metricString(sample.Metric), sample.Value, milliseconds)

		if c.cfg.pgPrometheusLogSamples {
			fmt.Println(line)
		}

		_, err = stmt.Exec(line)
		if err != nil {
			level.Error(c.logger).Log("msg", "Error executing statement", "stmt", line, "err", err)
			return err
		}

	}

	_, err = stmt.Exec()
	if err != nil {
		level.Error(c.logger).Log("msg", "Error executing close of copy", "err", err)
		return err
	}

	err = stmt.Close()

	if err != nil {
		level.Error(c.logger).Log("msg", "Error on Close when writing samples", "err", err)
		return err
	}

	err = tx.Commit()

	if err != nil {
		level.Error(c.logger).Log("msg", "Error on Commit when writing samples", "err", err)
		return err
	}

	duration := time.Since(begin).Seconds()

	level.Debug(c.logger).Log("msg", "Wrote samples", "count", len(samples), "duration", duration)

	return nil
}

type sampleLabels struct {
	JSON        []byte
	Map         map[string]string
	OrderedKeys []string
}

func createOrderedKeys(m *map[string]string) []string {
	keys := make([]string, 0, len(*m))
	for k := range *m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (l *sampleLabels) Scan(value interface{}) error {
	if value == nil {
		l = &sampleLabels{}
		return nil
	}

	switch t := value.(type) {
	case []uint8:
		m := make(map[string]string)
		err := json.Unmarshal(t, &m)

		if err != nil {
			return err
		}

		*l = sampleLabels{
			JSON:        t,
			Map:         m,
			OrderedKeys: createOrderedKeys(&m),
		}
		return nil
	}
	return fmt.Errorf("Invalid labels value %s", reflect.TypeOf(value))
}

func (l sampleLabels) String() string {
	return string(l.JSON)
}

func (l sampleLabels) key(extra string) string {
	// 0xff cannot cannot occur in valid UTF-8 sequences, so use it
	// as a separator here.
	separator := "\xff"
	pairs := make([]string, 0, len(l.Map)+1)
	pairs = append(pairs, extra+separator)

	for _, k := range l.OrderedKeys {
		pairs = append(pairs, k+separator+l.Map[k])
	}
	return strings.Join(pairs, separator)
}

func (l *sampleLabels) len() int {
	return len(l.OrderedKeys)
}

// Read implements the Reader interface and reads metrics samples from the database
func (c *Client) Read(req *prompb.ReadRequest) (*prompb.ReadResponse, error) {
	labelsToSeries := map[string]*prompb.TimeSeries{}

	for _, q := range req.Queries {
		command, err := c.buildCommand(q)

		if err != nil {
			return nil, err
		}

		level.Debug(c.logger).Log("msg", "Executed query", "query", command)

		rows, err := c.db.Query(command)

		if err != nil {
			return nil, err
		}

		defer rows.Close()

		for rows.Next() {
			var (
				value  float64
				name   string
				labels sampleLabels
				time   time.Time
			)
			err := rows.Scan(&time, &name, &value, &labels)

			if err != nil {
				return nil, err
			}

			key := labels.key(name)
			ts, ok := labelsToSeries[key]

			if !ok {
				labelPairs := make([]*prompb.Label, 0, labels.len()+1)
				labelPairs = append(labelPairs, &prompb.Label{
					Name:  model.MetricNameLabel,
					Value: name,
				})

				for _, k := range labels.OrderedKeys {
					labelPairs = append(labelPairs, &prompb.Label{
						Name:  k,
						Value: labels.Map[k],
					})
				}

				ts = &prompb.TimeSeries{
					Labels:  labelPairs,
					Samples: make([]*prompb.Sample, 0, 100),
				}
				labelsToSeries[key] = ts
			}

			ts.Samples = append(ts.Samples, &prompb.Sample{
				Timestamp: time.UnixNano() / 1000000,
				Value:     value,
			})
		}

		err = rows.Err()

		if err != nil {
			return nil, err
		}
	}

	resp := prompb.ReadResponse{
		Results: []*prompb.QueryResult{
			{
				Timeseries: make([]*prompb.TimeSeries, 0, len(labelsToSeries)),
			},
		},
	}
	for _, ts := range labelsToSeries {
		resp.Results[0].Timeseries = append(resp.Results[0].Timeseries, ts)
	}

	level.Debug(c.logger).Log("msg", "Returned response", "#timeseries", len(labelsToSeries))

	return &resp, nil
}

// HealthCheck implements the healtcheck interface
func (c *Client) HealthCheck() error {
	rows, err := c.db.Query("SELECT 1")

	if err != nil {
		level.Debug(c.logger).Log("msg", "Health check error", "err", err)
		return err
	}

	rows.Close()
	return nil
}

func toTimestamp(milliseconds int64) time.Time {
	sec := milliseconds / 1000
	nsec := (milliseconds - (sec * 1000)) * 1000000
	return time.Unix(sec, nsec)
}

func (c *Client) buildQuery(q *prompb.Query) (string, error) {
	matchers := make([]string, 0, len(q.Matchers))
	labelEqualPredicates := make(map[string]string)

	for _, m := range q.Matchers {
		escapedValue := escapeValue(m.Value)

		if m.Name == model.MetricNameLabel {
			switch m.Type {
			case prompb.LabelMatcher_EQ:
				if len(escapedValue) == 0 {
					matchers = append(matchers, fmt.Sprintf("(name IS NULL OR name = '')"))
				} else {
					matchers = append(matchers, fmt.Sprintf("name = '%s'", escapedValue))
				}
			case prompb.LabelMatcher_NEQ:
				matchers = append(matchers, fmt.Sprintf("name != '%s'", escapedValue))
			case prompb.LabelMatcher_RE:
				matchers = append(matchers, fmt.Sprintf("name ~ '%s'", anchorValue(escapedValue)))
			case prompb.LabelMatcher_NRE:
				matchers = append(matchers, fmt.Sprintf("name !~ '%s'", anchorValue(escapedValue)))
			default:
				return "", fmt.Errorf("unknown metric name match type %v", m.Type)
			}
		} else {
			switch m.Type {
			case prompb.LabelMatcher_EQ:
				if len(escapedValue) == 0 {
					// From the PromQL docs: "Label matchers that match
					// empty label values also select all time series that
					// do not have the specific label set at all."
					matchers = append(matchers, fmt.Sprintf("((labels ? '%s') = false OR (labels->>'%s' = ''))",
						m.Name, m.Name))
				} else {
					labelEqualPredicates[m.Name] = m.Value
				}
			case prompb.LabelMatcher_NEQ:
				matchers = append(matchers, fmt.Sprintf("labels->>'%s' != '%s'", m.Name, escapedValue))
			case prompb.LabelMatcher_RE:
				matchers = append(matchers, fmt.Sprintf("labels->>'%s' ~ '%s'", m.Name, anchorValue(escapedValue)))
			case prompb.LabelMatcher_NRE:
				matchers = append(matchers, fmt.Sprintf("labels->>'%s' !~ '%s'", m.Name, anchorValue(escapedValue)))
			default:
				return "", fmt.Errorf("unknown match type %v", m.Type)
			}
		}
	}
	equalsPredicate := ""

	if len(labelEqualPredicates) > 0 {
		labelsJSON, err := json.Marshal(labelEqualPredicates)

		if err != nil {
			return "", err
		}
		equalsPredicate = fmt.Sprintf(" AND labels @> '%s'", labelsJSON)
	}

	matchers = append(matchers, fmt.Sprintf("time >= '%v'", toTimestamp(q.StartTimestampMs).Format(time.RFC3339)))
	matchers = append(matchers, fmt.Sprintf("time <= '%v'", toTimestamp(q.EndTimestampMs).Format(time.RFC3339)))

	return fmt.Sprintf("SELECT time, name, value, labels FROM %s WHERE %s %s",
		c.cfg.table, strings.Join(matchers, " AND "), equalsPredicate), nil
}

func (c *Client) buildCommand(q *prompb.Query) (string, error) {
	return c.buildQuery(q)
}

func escapeValue(str string) string {
	return strings.Replace(str, `'`, `\'`, -1)
}

// anchorValue adds anchors to values in regexps since PromQL docs
// states that "Regex-matches are fully anchored."
func anchorValue(str string) string {
	l := len(str)

	if l == 0 || (str[0] == '^' && str[l-1] == '$') {
		return str
	}

	if str[0] == '^' {
		return fmt.Sprintf("%s$", str)
	}

	if str[l-1] == '$' {
		return fmt.Sprintf("^%s", str)
	}

	return fmt.Sprintf("^%s$", str)
}

// Name identifies the client as a PostgreSQL client.
func (c Client) Name() string {
	return "PostgreSQL"
}

// Describe implements prometheus.Collector.
func (c *Client) Describe(ch chan<- *prometheus.Desc) {
}

// Collect implements prometheus.Collector.
func (c *Client) Collect(ch chan<- prometheus.Metric) {
	//ch <- c.ignoredSamples
}
