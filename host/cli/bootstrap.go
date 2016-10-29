package cli

import (
	"archive/tar"
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
				if _, err := f.Seek(0, os.SEEK_SET); err != nil {
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
		ID       string
		Artifact struct {
			URI string
		}
		Release struct {
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

	artifactURIs := make(map[string]string)
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
		if step.Artifact.URI != "" {
			artifactURIs[step.ID] = step.Artifact.URI

			// update current artifact in database for service, taking care to
			// check the database version as migration 15 changed the way
			// artifacts are related to releases in the database
			sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT MAX(id) FROM schema_migrations) < 15 THEN
      UPDATE artifacts SET uri = '%[1]s' WHERE artifact_id = (
	SELECT artifact_id FROM releases WHERE release_id = (
	  SELECT release_id FROM apps WHERE name = '%[2]s'
	)
      );
    ELSE
      UPDATE artifacts SET uri = '%[1]s' WHERE type = 'docker' AND artifact_id = (
	SELECT artifact_id FROM release_artifacts WHERE release_id = (
	  SELECT release_id FROM apps WHERE name = '%[2]s'
	)
      );
    END IF;
  END;
$$;`, step.Artifact.URI, step.ID))
		}
	}

	data.Discoverd.ImageArtifact.URI = artifactURIs["discoverd"]
	data.Discoverd.Release.Env["DISCOVERD_PEERS"] = "{{ range $ip := .SortedHostIPs }}{{ $ip }}:1111,{{ end }}"
	data.Postgres.ImageArtifact.URI = artifactURIs["postgres"]
	data.Flannel.ImageArtifact.URI = artifactURIs["flannel"]
	data.Controller.ImageArtifact.URI = artifactURIs["controller"]
	if data.MariaDB != nil {
		data.MariaDB.ImageArtifact.URI = artifactURIs["mariadb"]
	}
	if data.MongoDB != nil {
		data.MongoDB.ImageArtifact.URI = artifactURIs["mongodb"]
	}

	// set TELEMETRY_CLUSTER_ID
	telemetryClusterID := random.UUID()
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{TELEMETRY_CLUSTER_ID}', '%q')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'controller');
`, telemetryClusterID))
	data.Controller.Release.Env["TELEMETRY_CLUSTER_ID"] = telemetryClusterID

	// set TELEMETRY_BOOTSTRAP_ID if unset
	if data.Controller.Release.Env["TELEMETRY_BOOTSTRAP_ID"] == "" {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{TELEMETRY_BOOTSTRAP_ID}', '%q')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'controller');
`, telemetryClusterID))
		data.Controller.Release.Env["TELEMETRY_BOOTSTRAP_ID"] = telemetryClusterID
	}

	// create the slugbuilder artifact if gitreceive still references
	// SLUGBUILDER_IMAGE_URI (in which case there is no slugbuilder
	// artifact in the database)
	sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT env->>'SLUGBUILDER_IMAGE_ID' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive')) IS NULL THEN
      INSERT INTO artifacts (artifact_id, type, uri) VALUES ('%s', 'docker', '%s');
    END IF;
  END;
$$;`, random.UUID(), artifactURIs["slugbuilder-image"]))

	// update the URI of slug artifacts currently being referenced by
	// gitreceive (which will also update all current user releases
	// to use the latest slugrunner)
	for _, name := range []string{"slugbuilder", "slugrunner"} {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%[1]s'
WHERE artifact_id = (SELECT (env->>'%[2]s_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive'))
OR uri = (SELECT env->>'%[2]s_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive'));`,
			artifactURIs[name+"-image"], strings.ToUpper(name)))
	}

	// update the URI of redis artifacts currently being referenced by
	// the redis app (which will also update all current redis
	// resources to use the latest redis image)
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%s'
WHERE artifact_id = (SELECT (env->>'REDIS_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis'))
OR uri = (SELECT env->>'REDIS_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis'));`,
		artifactURIs["redis-image"]))

	// ensure the image ID environment variables are set for legacy apps
	// which use image URI variables
	for _, name := range []string{"redis", "slugbuilder", "slugrunner"} {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = pg_temp.json_object_update_key(env, '%[1]s_IMAGE_ID', (SELECT artifact_id::text FROM artifacts WHERE uri = '%[2]s'))
WHERE env->>'%[1]s_IMAGE_URI' IS NOT NULL;`,
			strings.ToUpper(name), artifactURIs[name+"-image"]))
	}

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
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'flannel');
		`, network))
	}

	// start discoverd/flannel/postgres
	cfg.Singleton = data.Postgres.Release.Env["SINGLETON"] == "true"
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
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'discoverd');
`, state.StepData["discoverd"].(*bootstrap.RunAppState).Release.Env["DISCOVERD_PEERS"]))

	// make sure STATUS_KEY has the correct value in the dashboard release
	sqlBuf.WriteString(`
UPDATE releases SET env = jsonb_set(env, '{STATUS_KEY}', (
	SELECT env->'AUTH_KEY' FROM releases
	WHERE release_id = (SELECT release_id FROM apps WHERE name = 'status')
))
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'dashboard');
`)

	// load data into postgres
	cmd := exec.JobUsingHost(state.Hosts[0], host.Artifact{Type: data.Postgres.ImageArtifact.Type, URI: data.Postgres.ImageArtifact.URI}, nil)
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
	if os.Getenv("DEBUG") != "" {
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

	// wait for controller to come up
	meta = bootstrap.StepMeta{ID: "wait-controller", Action: "wait-controller"}
	ch <- &bootstrap.StepInfo{StepMeta: meta, State: "start", Timestamp: time.Now().UTC()}
	controllerInstances, err := discoverd.GetInstances("controller", 30*time.Second)
	if err != nil {
		return fmt.Errorf("error getting controller instance: %s", err)
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

		cmd = exec.JobUsingHost(state.Hosts[0], host.Artifact{Type: data.MariaDB.ImageArtifact.Type, URI: data.MariaDB.ImageArtifact.URI}, nil)
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

		cmd = exec.JobUsingHost(state.Hosts[0], host.Artifact{Type: data.MongoDB.ImageArtifact.Type, URI: data.MongoDB.ImageArtifact.URI}, nil)
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

	controllerKey := data.Controller.Release.Env["AUTH_KEY"]
	client, err := controller.NewClient("http://"+controllerInstances[0].Addr, controllerKey)
	if err != nil {
		return err
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

	// start blobstore, scheduler, and enable cluster monitor
	data.Controller.Processes = map[string]int{"scheduler": 1}
	// only start one scheduler instance
	schedulerProcess := data.Controller.Release.Processes["scheduler"]
	schedulerProcess.Omni = false
	data.Controller.Release.Processes["scheduler"] = schedulerProcess
	_, err = bootstrap.Manifest{
		step("blobstore", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: blobstoreFormation,
		}),
		step("blobstore-wait", "wait", &bootstrap.WaitAction{
			URL:    "http://blobstore.discoverd",
			Status: 200,
		}),
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
