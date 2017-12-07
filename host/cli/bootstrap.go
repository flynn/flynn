package cli

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/bootstrap"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("bootstrap", runBootstrap, `
usage: flynn-host bootstrap [options] [<manifest>]

Options:
  -n, --min-hosts=MIN  minimum number of hosts required to be online
  -t, --timeout=SECS   seconds to wait for hosts to come online [default: 120]
  --json               format log output as json
  --from-backup=FILE   bootstrap from backup file
  --discovery=TOKEN    use discovery token to connect to cluster
  --peer-ips=IPLIST    use IP address list to connect to cluster
  --steps=STEPS        only run the given STEPS (comma separated)

Bootstrap layer 1 using the provided manifest`)
}

func readBootstrapManifest(name string) ([]byte, error) {
	if name == "" || name == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	return ioutil.ReadFile(name)
}

var manifest []byte

func runBootstrap(args *docopt.Args) error {
	log.SetFlags(log.Lmicroseconds)
	logf := textLogger
	if args.Bool["--json"] {
		logf = jsonLogger
	}
	var cfg bootstrap.Config

	manifestFile := args.String["<manifest>"]
	if manifestFile == "" {
		manifestFile = "/etc/flynn/bootstrap-manifest.json"
	}

	var steps []string
	if s := args.String["--steps"]; s != "" {
		steps = strings.Split(s, ",")
	}

	var err error
	manifest, err = readBootstrapManifest(manifestFile)
	if err != nil {
		return fmt.Errorf("Error reading manifest: %s", err)
	}

	if n := args.String["--min-hosts"]; n != "" {
		if cfg.MinHosts, err = strconv.Atoi(n); err != nil || cfg.MinHosts < 1 {
			return fmt.Errorf("invalid --min-hosts value")
		}
	}

	cfg.Timeout, err = strconv.Atoi(args.String["--timeout"])
	if err != nil {
		return fmt.Errorf("invalid --timeout value")
	}

	if ipList := args.String["--peer-ips"]; ipList != "" {
		cfg.IPs = strings.Split(ipList, ",")
		if cfg.MinHosts == 0 {
			cfg.MinHosts = len(cfg.IPs)
		}
	}

	if cfg.MinHosts == 0 {
		cfg.MinHosts = 1
	}

	cfg.Singleton = cfg.MinHosts == 1
	if s := os.Getenv("SINGLETON"); s != "" {
		cfg.Singleton = s == "true"
	}

	ch := make(chan *bootstrap.StepInfo)
	done := make(chan struct{})
	var last error
	go func() {
		for si := range ch {
			logf(si)
			last = si.Err
		}
		close(done)
	}()

	cfg.ClusterURL = args.String["--discovery"]
	if bf := args.String["--from-backup"]; bf != "" {
		err = runBootstrapBackup(manifest, bf, ch, cfg)
	} else {
		err = bootstrap.Run(manifest, ch, cfg, steps)
	}

	<-done
	if err != nil && last != nil && err.Error() == last.Error() {
		return ErrAlreadyLogged{err}
	}
	return err
}

func runBootstrapBackup(manifest []byte, backupFile string, ch chan *bootstrap.StepInfo, cfg bootstrap.Config) error {
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	defer close(ch)
	f, err := os.Open(backupFile)
	if err != nil {
		return fmt.Errorf("error opening backup file: %s", err)
	}
	defer f.Close()
	tr := tar.NewReader(f)

	getFile := func(name string) (io.Reader, error) {
		rewound := false
		var res io.Reader
		for {
			header, err := tr.Next()
			if err == io.EOF && !rewound {
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return nil, fmt.Errorf("error seeking in backup file: %s", err)
				}
				rewound = true
				tr = tar.NewReader(f)
				continue
			} else if err != nil {
				return nil, fmt.Errorf("error finding %s in backup file: %s", name, err)
			}
			if path.Base(header.Name) != name {
				continue
			}
			if strings.HasSuffix(name, ".gz") {
				res, err = gzip.NewReader(tr)
				if err != nil {
					return nil, fmt.Errorf("error opening %s from backup file: %s", name, err)
				}
			} else {
				res = tr
			}
			break
		}
		return res, nil
	}

	var data struct {
		Discoverd, Flannel, Postgres, MariaDB, MongoDB, Controller *ct.ExpandedFormation
	}

	jsonData, err := getFile("flynn.json")
	if err != nil {
		return err
	}
	if jsonData == nil {
		return fmt.Errorf("did not file flynn.json in backup file")
	}
	if err := json.NewDecoder(jsonData).Decode(&data); err != nil {
		return fmt.Errorf("error decoding backup data: %s", err)
	}

	db, err := getFile("postgres.sql.gz")
	if err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("did not find postgres.sql.gz in backup file")
	}

	// add buffer to the end of the SQL import containing commands that rewrite data in the controller db
	sqlBuf := &bytes.Buffer{}
	db = io.MultiReader(db, sqlBuf)
	sqlBuf.WriteString(fmt.Sprintf("\\connect %s\n", data.Controller.Release.Env["PGDATABASE"]))
	sqlBuf.WriteString(`
CREATE FUNCTION pg_temp.json_object_update_key(
  "json"          jsonb,
  "key_to_set"    TEXT,
  "value_to_set"  TEXT
)
  RETURNS jsonb
  LANGUAGE sql
  IMMUTABLE
  STRICT
AS $function$
     SELECT ('{' || string_agg(to_json("key") || ':' || "value", ',') || '}')::jsonb
       FROM (SELECT *
               FROM json_each("json"::json)
              WHERE "key" <> "key_to_set"
              UNION ALL
             SELECT "key_to_set", to_json("value_to_set")) AS "fields"
$function$;
`)

	type manifestStep struct {
		ID        string
		Artifacts []*ct.Artifact
		Artifact  *ct.Artifact
		Release   struct {
			Env       map[string]string
			Processes map[string]ct.ProcessType
		}
	}
	var manifestSteps []*manifestStep
	if err := json.Unmarshal(manifest, &manifestSteps); err != nil {
		return fmt.Errorf("error decoding manifest json: %s", err)
	}

	manifestStepMap := make(map[string]bootstrap.Step, len(manifestSteps))
	steps, err := bootstrap.UnmarshalManifest(manifest, nil)
	if err != nil {
		return fmt.Errorf("error decoding manifest json: %s", err)
	}
	for _, step := range steps {
		manifestStepMap[step.StepMeta.ID] = step
	}

	artifacts := make(map[string]*ct.Artifact)
	updateProcArgs := func(f *ct.ExpandedFormation, step *manifestStep) {
		for typ, proc := range step.Release.Processes {
			p := f.Release.Processes[typ]
			p.Args = proc.Args
			f.Release.Processes[typ] = p
		}
	}
	updateVolumes := func(f *ct.ExpandedFormation, step *manifestStep) {
		for typ, proc := range step.Release.Processes {
			p := f.Release.Processes[typ]
			p.Volumes = proc.Volumes
			f.Release.Processes[typ] = p
		}
	}
	for _, step := range manifestSteps {
		switch step.ID {
		case "discoverd":
			updateVolumes(data.Discoverd, step)
		case "postgres":
			updateProcArgs(data.Postgres, step)
			updateVolumes(data.Postgres, step)
		case "controller":
			updateProcArgs(data.Controller, step)
		case "mariadb":
			if data.MariaDB != nil {
				updateProcArgs(data.MariaDB, step)
				updateVolumes(data.MariaDB, step)
			}
		case "mongodb":
			if data.MongoDB != nil {
				updateProcArgs(data.MongoDB, step)
				updateVolumes(data.MongoDB, step)
			}
		}
		if step.Artifact != nil {
			artifacts[step.ID] = step.Artifact
		} else if len(step.Artifacts) > 0 {
			artifacts[step.ID] = step.Artifacts[0]
		}
	}

	data.Discoverd.Artifacts = []*ct.Artifact{artifacts["discoverd"]}
	data.Discoverd.Release.Env["DISCOVERD_PEERS"] = "{{ range $ip := .SortedHostIPs }}{{ $ip }}:1111,{{ end }}"
	data.Postgres.Artifacts = []*ct.Artifact{artifacts["postgres"]}
	data.Flannel.Artifacts = []*ct.Artifact{artifacts["flannel"]}
	data.Controller.Artifacts = []*ct.Artifact{artifacts["controller"]}
	if data.MariaDB != nil {
		data.MariaDB.Artifacts = []*ct.Artifact{artifacts["mariadb"]}
	}
	if data.MongoDB != nil {
		data.MongoDB.Artifacts = []*ct.Artifact{artifacts["mongodb"]}
	}

	// set TELEMETRY_CLUSTER_ID
	telemetryClusterID := random.UUID()
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{TELEMETRY_CLUSTER_ID}', '%q')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'controller' AND deleted_at IS NULL);
`, telemetryClusterID))
	data.Controller.Release.Env["TELEMETRY_CLUSTER_ID"] = telemetryClusterID

	// set TELEMETRY_BOOTSTRAP_ID if unset
	if data.Controller.Release.Env["TELEMETRY_BOOTSTRAP_ID"] == "" {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{TELEMETRY_BOOTSTRAP_ID}', '%q')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'controller' AND deleted_at IS NULL);
`, telemetryClusterID))
		data.Controller.Release.Env["TELEMETRY_BOOTSTRAP_ID"] = telemetryClusterID
	}

	// update logaggregator args
	sqlBuf.WriteString(`
UPDATE releases SET processes = jsonb_set(processes, '{app,args}', '["/bin/logaggregator"]')
WHERE release_id IN (SELECT release_id FROM apps WHERE name = 'logaggregator' AND deleted_at IS NULL);
`)

	step := func(id, name string, action bootstrap.Action) bootstrap.Step {
		if ra, ok := action.(*bootstrap.RunAppAction); ok {
			ra.ID = id
		}
		return bootstrap.Step{
			StepMeta: bootstrap.StepMeta{ID: id, Action: name},
			Action:   action,
		}
	}

	// ensure flannel has NETWORK set if required
	if network := os.Getenv("FLANNEL_NETWORK"); network != "" {
		data.Flannel.Release.Env["NETWORK"] = network
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = pg_temp.json_object_update_key(env, 'NETWORK', '%s')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'flannel' AND deleted_at IS NULL);
		`, network))
	}

	// ensure controller / gitreceive have tmp volumes
	sqlBuf.WriteString(`
UPDATE releases SET processes = jsonb_set(processes, '{web,volumes}', '[{"path": "/tmp", "delete_on_stop": true}]')
WHERE release_id IN (SELECT release_id FROM apps WHERE name = 'controller' AND deleted_at IS NULL);
UPDATE releases SET processes = jsonb_set(processes, '{app,volumes}', '[{"path": "/tmp", "delete_on_stop": true}]')
WHERE release_id IN (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL);
`)

	// update the SINGLETON environment variable for database appliances
	// (which includes updating legacy appliances which had SINGLETON set
	// on the database type rather than the release)
	singleton := strconv.FormatBool(cfg.Singleton)
	data.Postgres.Release.Env["SINGLETON"] = singleton
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{SINGLETON}', '%q')
WHERE release_id IN (SELECT release_id FROM apps WHERE name IN ('postgres', 'mariadb', 'mongodb'));
`, singleton))

	if data.MariaDB != nil {
		data.MariaDB.Release.Env["SINGLETON"] = singleton
		delete(data.MariaDB.Release.Processes["mariadb"].Env, "SINGLETON")
		sqlBuf.WriteString(`
DO $$
  BEGIN
    IF (SELECT processes->'mariadb' ? 'env' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'mariadb' AND deleted_at IS NULL)) THEN
      UPDATE releases SET processes = jsonb_set(processes, '{mariadb,env}', (processes #> '{mariadb,env}')::jsonb - 'SINGLETON')
      WHERE release_id IN (SELECT release_id FROM apps WHERE name = 'mariadb' AND deleted_at IS NULL);
    END IF;
  END;
$$;`)
	}

	if data.MongoDB != nil {
		data.MongoDB.Release.Env["SINGLETON"] = singleton
		delete(data.MongoDB.Release.Processes["mongodb"].Env, "SINGLETON")
		sqlBuf.WriteString(`
DO $$
  BEGIN
    IF (SELECT processes->'mongodb' ? 'env' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'mongodb' AND deleted_at IS NULL)) THEN
      UPDATE releases SET processes = jsonb_set(processes, '{mongodb,env}', (processes #> '{mongodb,env}')::jsonb - 'SINGLETON')
      WHERE release_id IN (SELECT release_id FROM apps WHERE name = 'mongodb' AND deleted_at IS NULL);
    END IF;
  END;
$$;`)
	}

	// modify app scale based on whether we are booting
	// a singleton or HA cluster
	var scale map[string]map[string]int
	if cfg.Singleton {
		scale = map[string]map[string]int{
			"postgres":       {"postgres": 1, "web": 1},
			"mariadb":        {"web": 1},
			"mongodb":        {"web": 1},
			"controller":     {"web": 1, "worker": 1},
			"redis":          {"web": 1},
			"blobstore":      {"web": 1},
			"gitreceive":     {"app": 1},
			"docker-receive": {"app": 1},
			"logaggregator":  {"app": 1},
			"dashboard":      {"web": 1},
			"status":         {"web": 1},
		}
		data.Postgres.Processes["postgres"] = 1
		data.Postgres.Processes["web"] = 1
		if data.MariaDB != nil {
			data.MariaDB.Processes["mariadb"] = 1
			data.MariaDB.Processes["web"] = 1
		}
		if data.MongoDB != nil {
			data.MongoDB.Processes["mongodb"] = 1
			data.MongoDB.Processes["web"] = 1
		}
	} else {
		scale = map[string]map[string]int{
			"postgres":       {"postgres": 3, "web": 2},
			"mariadb":        {"web": 2},
			"mongodb":        {"web": 2},
			"controller":     {"web": 2, "worker": 2},
			"redis":          {"web": 2},
			"blobstore":      {"web": 2},
			"gitreceive":     {"app": 2},
			"docker-receive": {"app": 2},
			"logaggregator":  {"app": 2},
			"dashboard":      {"web": 2},
			"status":         {"web": 2},
		}
		data.Postgres.Processes["postgres"] = 3
		data.Postgres.Processes["web"] = 2
		if data.MariaDB != nil {
			data.MariaDB.Processes["mariadb"] = 3
			data.MariaDB.Processes["web"] = 2
		}
		if data.MongoDB != nil {
			data.MongoDB.Processes["mongodb"] = 3
			data.MongoDB.Processes["web"] = 2
		}
	}
	for app, procs := range scale {
		for typ, count := range procs {
			sqlBuf.WriteString(fmt.Sprintf(`
UPDATE formations SET processes = jsonb_set(processes, '{%s}', '%d')
WHERE release_id = (SELECT release_id FROM apps WHERE name = '%s' AND deleted_at IS NULL);
`, typ, count, app))
		}
	}

	// start discoverd/flannel/postgres
	systemSteps := bootstrap.Manifest{
		step("discoverd", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.Discoverd,
		}),
		step("flannel", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.Flannel,
		}),
		step("wait-hosts", "wait-hosts", &bootstrap.WaitHostsAction{}),
		step("postgres", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.Postgres,
		}),
		step("postgres-wait", "sirenia-wait", &bootstrap.SireniaWaitAction{
			Service: "postgres",
		}),
	}

	state, err := systemSteps.Run(ch, cfg)
	if err != nil {
		return err
	}

	// set DISCOVERD_PEERS in release
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = pg_temp.json_object_update_key(env, 'DISCOVERD_PEERS', '%s')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'discoverd' AND deleted_at IS NULL);
`, state.StepData["discoverd"].(*bootstrap.RunAppState).Release.Env["DISCOVERD_PEERS"]))

	// make sure STATUS_KEY has the correct value in the dashboard release
	sqlBuf.WriteString(`
UPDATE releases SET env = jsonb_set(env, '{STATUS_KEY}', (
	SELECT env->'AUTH_KEY' FROM releases
	WHERE release_id = (SELECT release_id FROM apps WHERE name = 'status' AND deleted_at IS NULL)
))
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'dashboard' AND deleted_at IS NULL);
`)

	// load data into postgres
	cmd := exec.JobUsingHost(state.Hosts[0], artifacts["postgres"], nil)
	cmd.Args = []string{"psql"}
	cmd.Env = map[string]string{
		"PGHOST":     "leader.postgres.discoverd",
		"PGUSER":     "flynn",
		"PGDATABASE": "postgres",
		"PGPASSWORD": data.Postgres.Release.Env["PGPASSWORD"],
	}
	cmd.Stdin = db
	meta := bootstrap.StepMeta{ID: "restore", Action: "restore-postgres"}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
	out, err := cmd.CombinedOutput()
	if os.Getenv("DEBUG") == "true" {
		fmt.Println(string(out))
	}
	if err != nil {
		ch <- &bootstrap.StepInfo{
			StepMeta:  meta,
			State:     "error",
			Error:     fmt.Sprintf("error running psql restore: %s - %q", err, string(out)),
			Err:       err,
			Timestamp: time.Now().UTC(),
		}
		return err
	}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}

	// start controller API
	data.Controller.Processes = map[string]int{"web": 1}
	_, err = bootstrap.Manifest{
		step("controller", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.Controller,
		}),
	}.RunWithState(ch, state)
	if err != nil {
		return err
	}

	// wait for controller to come up
	meta = bootstrap.StepMeta{ID: "wait-controller", Action: "wait-controller"}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
	controllerInstances, err := discoverd.GetInstances("controller", 30*time.Second)
	if err != nil {
		return fmt.Errorf("error getting controller instance: %s", err)
	}
	controllerKey := data.Controller.Release.Env["AUTH_KEY"]
	client, err := controller.NewClient("http://"+controllerInstances[0].Addr, controllerKey)
	if err != nil {
		return err
	}

	// start mariadb and load data if it was present in the backup.
	mysqldb, err := getFile("mysql.sql.gz")
	if err == nil && data.MariaDB != nil {
		_, err = bootstrap.Manifest{
			step("mariadb", "run-app", &bootstrap.RunAppAction{
				ExpandedFormation: data.MariaDB,
			}),
			step("mariadb-wait", "sirenia-wait", &bootstrap.SireniaWaitAction{
				Service: "mariadb",
			}),
		}.RunWithState(ch, state)
		if err != nil {
			return err
		}

		// ensure the formation is correct in the database
		if err := client.PutFormation(data.MariaDB.Formation()); err != nil {
			return fmt.Errorf("error updating mariadb formation: %s", err)
		}

		cmd = exec.JobUsingHost(state.Hosts[0], artifacts["mariadb"], nil)
		cmd.Args = []string{"mysql", "-u", "flynn", "-h", "leader.mariadb.discoverd"}
		cmd.Env = map[string]string{
			"MYSQL_PWD": data.MariaDB.Release.Env["MYSQL_PWD"],
		}
		cmd.Stdin = mysqldb
		meta = bootstrap.StepMeta{ID: "restore", Action: "restore-mariadb"}
		ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
		out, err = cmd.CombinedOutput()
		if os.Getenv("DEBUG") != "" {
			fmt.Println(string(out))
		}
		if err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error running mysql restore: %s - %q", err, string(out)),
				Err:       err,
				Timestamp: time.Now().UTC(),
			}
			return err
		}
		ch <- &bootstrap.StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}
	}

	// start mongodb and load data if it was present in the backup.
	mongodb, err := getFile("mongodb.archive.gz")
	if err == nil && data.MongoDB != nil {
		_, err = bootstrap.Manifest{
			step("mongodb", "run-app", &bootstrap.RunAppAction{
				ExpandedFormation: data.MongoDB,
			}),
			step("mongodb-wait", "sirenia-wait", &bootstrap.SireniaWaitAction{
				Service: "mongodb",
			}),
		}.RunWithState(ch, state)
		if err != nil {
			return err
		}

		// ensure the formation is correct in the database
		if err := client.PutFormation(data.MongoDB.Formation()); err != nil {
			return fmt.Errorf("error updating mongodb formation: %s", err)
		}

		cmd = exec.JobUsingHost(state.Hosts[0], artifacts["mongodb"], nil)
		cmd.Args = []string{"mongorestore", "-h", "leader.mongodb.discoverd", "-u", "flynn", "-p", data.MongoDB.Release.Env["MONGO_PWD"], "--archive"}
		cmd.Stdin = mongodb
		meta = bootstrap.StepMeta{ID: "restore", Action: "restore-mongodb"}
		ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
		out, err = cmd.CombinedOutput()
		if os.Getenv("DEBUG") != "" {
			fmt.Println(string(out))
		}
		if err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error running mongodb restore: %s - %q", err, string(out)),
				Err:       err,
				Timestamp: time.Now().UTC(),
			}
			return err
		}
		ch <- &bootstrap.StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}
	}

	// get blobstore config
	blobstoreRelease, err := client.GetAppRelease("blobstore")
	if err != nil {
		return fmt.Errorf("error getting blobstore release: %s", err)
	}
	blobstoreFormation, err := client.GetExpandedFormation("blobstore", blobstoreRelease.ID)
	if err != nil {
		return fmt.Errorf("error getting blobstore expanded formation: %s", err)
	}
	state.SetControllerKey(controllerKey)
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}

	// start the blobstore
	blobstoreFormation.Artifacts = []*ct.Artifact{artifacts["blobstore"]}
	_, err = bootstrap.Manifest{
		step("blobstore", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: blobstoreFormation,
		}),
		step("blobstore-wait", "wait", &bootstrap.WaitAction{
			URL:    "http://blobstore.discoverd",
			Status: 200,
		}),
	}.RunWithState(ch, state)
	if err != nil {
		return err
	}

	// now that the controller and blobstore are up and controller
	// migrations have run (so we know artifacts have a manifest column),
	// migrate all artifacts to Flynn images
	jsonb := func(v interface{}) []byte {
		data, _ := json.Marshal(v)
		return data
	}
	sqlBuf.Reset()
	for _, step := range manifestSteps {
		artifact, ok := artifacts[step.ID]
		if !ok {
			continue
		}

		// update current artifact in database for service
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%s', type = 'flynn', manifest = '%s', hashes = '%s', size = %d, layer_url_template = '%s', meta = '%s' WHERE artifact_id = (
  SELECT artifact_id FROM release_artifacts WHERE release_id = (
    SELECT release_id FROM apps WHERE name = '%s' AND deleted_at IS NULL
  )
);`, artifact.URI, jsonb(&artifact.RawManifest), jsonb(artifact.Hashes), artifact.Size, artifact.LayerURLTemplate, jsonb(artifact.Meta), step.ID))
	}

	// create the slugbuilder artifact if gitreceive still references it by
	// URI (in which case there is no slugbuilder artifact in the database)
	slugBuilder := artifacts["slugbuilder-image"]
	sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT env->>'SLUGBUILDER_IMAGE_ID' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL)) IS NULL THEN
      INSERT INTO artifacts (artifact_id, type, uri, manifest, hashes, size, layer_url_template, meta) VALUES ('%s', 'flynn', '%s', '%s', '%s', %d, '%s', '%s');
    END IF;
  END;
$$;`, random.UUID(), slugBuilder.URI, jsonb(&slugBuilder.RawManifest), jsonb(slugBuilder.Hashes), slugBuilder.Size, slugBuilder.LayerURLTemplate, jsonb(slugBuilder.Meta)))

	// create the slugrunner artifact if it doesn't exist (which can be the
	// case if no apps were deployed with git push in older clusters where
	// it was created lazily)
	slugRunner := artifacts["slugrunner-image"]
	sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT env->>'SLUGRUNNER_IMAGE_ID' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL)) IS NULL THEN
      IF NOT EXISTS (SELECT 1 FROM artifacts WHERE uri = (SELECT env->>'SLUGRUNNER_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL))) THEN
        INSERT INTO artifacts (artifact_id, type, uri, manifest, hashes, size, layer_url_template, meta) VALUES ('%s', 'flynn', '%s', '%s', '%s', %d, '%s', '%s');
      END IF;
    END IF;
  END;
$$;`, random.UUID(), slugRunner.URI, jsonb(&slugRunner.RawManifest), jsonb(slugRunner.Hashes), slugRunner.Size, slugRunner.LayerURLTemplate, jsonb(slugRunner.Meta)))

	// update slug artifacts currently being referenced by gitreceive
	// (which will also update all current user releases to use the
	// latest slugrunner)
	for _, name := range []string{"slugbuilder", "slugrunner"} {
		artifact := artifacts[name+"-image"]
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%[1]s', type = 'flynn', manifest = '%[2]s', hashes = '%[3]s', size = %[4]d, layer_url_template = '%[5]s', meta = '%[6]s'
WHERE artifact_id = (SELECT (env->>'%[7]s_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL))
OR uri = (SELECT env->>'%[7]s_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive' AND deleted_at IS NULL));`,
			artifact.URI, jsonb(&artifact.RawManifest), jsonb(artifact.Hashes), artifact.Size, artifact.LayerURLTemplate, jsonb(artifact.Meta), strings.ToUpper(name)))
	}

	// update the URI of redis artifacts currently being referenced by
	// the redis app (which will also update all current redis resources
	// to use the latest redis image)
	redisImage := artifacts["redis-image"]
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%s', type = 'flynn', manifest = '%s', hashes = '%s', size = %d, layer_url_template = '%s', meta = '%s'
WHERE artifact_id = (SELECT (env->>'REDIS_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis' AND deleted_at IS NULL))
OR uri = (SELECT env->>'REDIS_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis' AND deleted_at IS NULL));`,
		redisImage.URI, jsonb(&redisImage.RawManifest), jsonb(redisImage.Hashes), redisImage.Size, redisImage.LayerURLTemplate, jsonb(redisImage.Meta)))

	// ensure the image ID environment variables are set for legacy apps
	// which use image URI variables
	for _, name := range []string{"redis", "slugbuilder", "slugrunner"} {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{%[1]s_IMAGE_ID}', ('"' || (SELECT artifact_id::text FROM artifacts WHERE uri = '%[2]s') || '"')::jsonb, true)
WHERE env->>'%[1]s_IMAGE_URI' IS NOT NULL;`,
			strings.ToUpper(name), artifacts[name+"-image"].URI))
	}

	// remove job and volume records created by previous clusters
	sqlBuf.WriteString(fmt.Sprintf(`
DELETE FROM events WHERE object_type IN ('job', 'volume') AND created_at < '%s';`,
		startedAt))
	sqlBuf.WriteString(fmt.Sprintf(`
DELETE FROM job_volumes USING job_cache WHERE job_volumes.job_id = job_cache.job_id AND job_cache.created_at < '%s';`,
		startedAt))
	sqlBuf.WriteString(fmt.Sprintf(`
DELETE FROM job_volumes USING volumes WHERE job_volumes.volume_id = volumes.volume_id AND volumes.created_at < '%s';`,
		startedAt))
	sqlBuf.WriteString(fmt.Sprintf(`
DELETE FROM job_cache WHERE created_at < '%s';`,
		startedAt))
	sqlBuf.WriteString(fmt.Sprintf(`
DELETE FROM volumes WHERE created_at < '%s';`,
		startedAt))

	// run the above artifact migration SQL against the controller database
	cmd = exec.JobUsingHost(state.Hosts[0], artifacts["postgres"], nil)
	cmd.Args = []string{"psql", "--echo-queries"}
	cmd.Env = map[string]string{
		"PGHOST":     "leader.postgres.discoverd",
		"PGUSER":     data.Controller.Release.Env["PGUSER"],
		"PGDATABASE": data.Controller.Release.Env["PGDATABASE"],
		"PGPASSWORD": data.Controller.Release.Env["PGPASSWORD"],
	}
	cmd.Stdin = sqlBuf
	meta = bootstrap.StepMeta{ID: "migrate-artifacts", Action: "migrate-artifacts"}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
	out, err = cmd.CombinedOutput()
	if os.Getenv("DEBUG") != "" {
		fmt.Println(string(out))
	}
	if err != nil {
		ch <- &bootstrap.StepInfo{
			StepMeta:  meta,
			State:     "error",
			Error:     fmt.Sprintf("error migrating artifacts: %s - %q", err, string(out)),
			Err:       err,
			Timestamp: time.Now().UTC(),
		}
		return err
	}

	// determine if there are any slugs or docker images which need to be
	// converted to Flynn images
	migrateSlugs := false
	migrateDocker := false
	artifactList, err := client.ArtifactList()
	if err != nil {
		return fmt.Errorf("error listing artifacts: %s", err)
	}
	for _, artifact := range artifactList {
		if artifact.Type == ct.DeprecatedArtifactTypeFile {
			migrateSlugs = true
		}
		if artifact.Type == ct.DeprecatedArtifactTypeDocker && artifact.Meta["docker-receive.repository"] != "" {
			migrateDocker = true
		}
		if migrateSlugs && migrateDocker {
			break
		}
	}

	runMigrator := func(cmd *exec.Cmd) error {
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			s := bufio.NewScanner(out)
			for s.Scan() {
				ch <- &bootstrap.StepInfo{
					StepMeta:  meta,
					State:     "info",
					StepData:  s.Text(),
					Timestamp: time.Now().UTC(),
				}
			}
		}()
		err = cmd.Run()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return err
	}

	if migrateSlugs {
		cmd = exec.JobUsingHost(state.Hosts[0], artifacts["slugbuilder-image"], nil)
		cmd.Args = []string{"/bin/slug-migrator"}
		cmd.Env = map[string]string{
			"CONTROLLER_KEY": data.Controller.Release.Env["AUTH_KEY"],
			"FLYNN_POSTGRES": data.Controller.Release.Env["FLYNN_POSTGRES"],
			"PGHOST":         "leader.postgres.discoverd",
			"PGUSER":         data.Controller.Release.Env["PGUSER"],
			"PGDATABASE":     data.Controller.Release.Env["PGDATABASE"],
			"PGPASSWORD":     data.Controller.Release.Env["PGPASSWORD"],
		}
		if err := runMigrator(cmd); err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error migrating slugs: %s", err),
				Err:       err,
				Timestamp: time.Now().UTC(),
			}
			return err
		}
	}

	if migrateDocker {
		// start docker-receive
		dockerRelease, err := client.GetAppRelease("docker-receive")
		if err != nil {
			return fmt.Errorf("error getting docker-receive release: %s", err)
		}
		dockerFormation, err := client.GetExpandedFormation("docker-receive", dockerRelease.ID)
		if err != nil {
			return fmt.Errorf("error getting docker-receive expanded formation: %s", err)
		}
		dockerFormation.Artifacts = []*ct.Artifact{artifacts["docker-receive"]}
		_, err = bootstrap.Manifest{
			step("docker-receive", "run-app", &bootstrap.RunAppAction{
				ExpandedFormation: dockerFormation,
			}),
			step("docker-receive-wait", "wait", &bootstrap.WaitAction{
				URL:    "http://docker-receive.discoverd/v2/",
				Status: 401,
			}),
		}.RunWithState(ch, state)
		if err != nil {
			return err
		}

		// run the docker image migrator
		cmd = exec.JobUsingHost(state.Hosts[0], artifacts["docker-receive"], nil)
		cmd.Args = []string{"/bin/docker-migrator"}
		cmd.Env = map[string]string{
			"CONTROLLER_KEY": data.Controller.Release.Env["AUTH_KEY"],
			"FLYNN_POSTGRES": data.Controller.Release.Env["FLYNN_POSTGRES"],
			"PGHOST":         "leader.postgres.discoverd",
			"PGUSER":         data.Controller.Release.Env["PGUSER"],
			"PGDATABASE":     data.Controller.Release.Env["PGDATABASE"],
			"PGPASSWORD":     data.Controller.Release.Env["PGPASSWORD"],
		}
		cmd.Volumes = []*ct.VolumeReq{{Path: "/tmp", DeleteOnStop: true}}

		// the job needs CAP_SYS_ADMIN so it can convert AUFS opaque
		// directories using setxattr(2)
		cmd.LinuxCapabilities = append(host.DefaultCapabilities, "CAP_SYS_ADMIN")
		if err := runMigrator(cmd); err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error migrating Docker images: %s", err),
				Err:       err,
				Timestamp: time.Now().UTC(),
			}
			return err
		}
	}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "done", Timestamp: time.Now().UTC()}

	// start scheduler and enable cluster monitor
	data.Controller.Processes = map[string]int{"scheduler": 1}
	// only start one scheduler instance
	schedulerProcess := data.Controller.Release.Processes["scheduler"]
	schedulerProcess.Omni = false
	data.Controller.Release.Processes["scheduler"] = schedulerProcess
	_, err = bootstrap.Manifest{
		step("controller-scheduler", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.Controller,
		}),
		step("status", "status-check", &bootstrap.StatusCheckAction{
			URL:     "http://status-web.discoverd",
			Timeout: 600,
		}),
		step("cluster-monitor", "cluster-monitor", &bootstrap.ClusterMonitorAction{
			Enabled: true,
		}),
	}.RunWithState(ch, state)
	if err != nil {
		return err
	}

	// mariadb and mongodb steps require the controller key
	state.StepData["controller-key"] = &bootstrap.RandomData{controllerKey}

	// deploy mariadb if it wasn't restored from the backup
	if data.MariaDB == nil {
		steps := bootstrap.Manifest{
			manifestStepMap["mariadb-password"],
			manifestStepMap["mariadb"],
			manifestStepMap["add-mysql-provider"],
			manifestStepMap["mariadb-wait"],
		}
		if _, err := steps.RunWithState(ch, state); err != nil {
			return fmt.Errorf("error deploying mariadb: %s", err)
		}
	}

	// deploy mongodb if it wasn't restored from the backup
	if data.MongoDB == nil {
		steps := bootstrap.Manifest{
			manifestStepMap["mongodb-password"],
			manifestStepMap["mongodb"],
			manifestStepMap["add-mongodb-provider"],
			manifestStepMap["mongodb-wait"],
		}
		if _, err := steps.RunWithState(ch, state); err != nil {
			return fmt.Errorf("error deploying mongodb: %s", err)
		}
	}

	// deploy docker-receive if it wasn't in the backup
	if _, err := client.GetApp("docker-receive"); err == controller.ErrNotFound {
		routes, err := client.RouteList("controller")
		if len(routes) == 0 {
			err = errors.New("no routes found")
		}
		if err != nil {
			return fmt.Errorf("error listing controller routes: %s", err)
		}
		for _, r := range routes {
			if r.Domain == fmt.Sprintf("controller.%s", data.Controller.Release.Env["DEFAULT_ROUTE_DOMAIN"]) {
				state.StepData["controller-cert"] = &tlscert.Cert{
					Cert:       r.Certificate.Cert,
					PrivateKey: r.Certificate.Key,
				}
				break
			}
		}
		steps := bootstrap.Manifest{
			manifestStepMap["docker-receive-secret"],
			manifestStepMap["docker-receive"],
			manifestStepMap["docker-receive-route"],
			manifestStepMap["docker-receive-wait"],
		}
		if _, err := steps.RunWithState(ch, state); err != nil {
			return fmt.Errorf("error deploying docker-receive: %s", err)
		}
	}

	return nil
}

func highlightBytePosition(manifest []byte, pos int64) (line, col int, highlight string) {
	// This function a modified version of a function in Camlistore written by Brad Fitzpatrick
	// https://github.com/bradfitz/camlistore/blob/830c6966a11ddb7834a05b6106b2530284a4d036/pkg/errorutil/highlight.go
	line = 1
	var lastLine string
	var currLine bytes.Buffer
	for i := int64(0); i < pos; i++ {
		b := manifest[i]
		if b == '\n' {
			lastLine = currLine.String()
			currLine.Reset()
			line++
			col = 1
		} else {
			col++
			currLine.WriteByte(b)
		}
	}
	if line > 1 {
		highlight += fmt.Sprintf("%5d: %s\n", line-1, lastLine)
	}
	highlight += fmt.Sprintf("%5d: %s\n", line, currLine.String())
	highlight += fmt.Sprintf("%s^\n", strings.Repeat(" ", col+5))
	return
}

func textLogger(si *bootstrap.StepInfo) {
	switch si.State {
	case "start":
		log.Printf("%s %s", si.Action, si.ID)
	case "info":
		log.Printf("%s %s", si.Action, si.StepData)
	case "done":
		if s, ok := si.StepData.(fmt.Stringer); ok {
			log.Printf("%s %s %s", si.Action, si.ID, s)
		}
	case "error":
		if serr, ok := si.Err.(*json.SyntaxError); ok {
			line, col, highlight := highlightBytePosition(manifest, serr.Offset)
			fmt.Printf("Error parsing JSON: %s\nAt line %d, column %d (offset %d):\n%s", si.Err, line, col, serr.Offset, highlight)
			return
		}
		log.Printf("%s %s error: %s", si.Action, si.ID, si.Error)
	}
}

func jsonLogger(si *bootstrap.StepInfo) {
	json.NewEncoder(os.Stdout).Encode(si)
}
