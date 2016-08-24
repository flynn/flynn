package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/bootstrap"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
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

	artifacts := make(map[string]*ct.Artifact)
	updateProcArgs := func(f *ct.ExpandedFormation, step *manifestStep) {
		for typ, proc := range step.Release.Processes {
			p := f.Release.Processes[typ]
			p.Args = proc.Args
			f.Release.Processes[typ] = p
		}
	}
	for _, step := range manifestSteps {
		switch step.ID {
		case "postgres":
			updateProcArgs(data.Postgres, step)
		case "controller":
			updateProcArgs(data.Controller, step)
		case "mariadb":
			if data.MariaDB != nil {
				updateProcArgs(data.MariaDB, step)
			}
		case "mongodb":
			if data.MongoDB != nil {
				updateProcArgs(data.MongoDB, step)
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
		if data.MariaDB.Processes["mariadb"] == 0 {
			// skip mariadb if it wasn't scaled up in the backup
			data.MariaDB = nil
		}
	}
	if data.MongoDB != nil {
		data.MongoDB.Artifacts = []*ct.Artifact{artifacts["mongodb"]}
		if data.MongoDB.Processes["mongodb"] == 0 {
			// skip mongodb if it wasn't scaled up in the backup
			data.MongoDB = nil
		}
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

	// start discoverd/flannel/postgres/mariadb
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
		step("postgres-wait", "wait", &bootstrap.WaitAction{
			URL: "http://postgres-api.discoverd/ping",
		}),
	}

	// Only run up MariaDB if it's in the backup
	if data.MariaDB != nil {
		systemSteps = append(systemSteps, step("mariadb", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.MariaDB,
		}))
		systemSteps = append(systemSteps, step("mariadb-wait", "wait", &bootstrap.WaitAction{
			URL: "http://mariadb-api.discoverd/ping",
		}))
	}

	// Only run up MongoDB if it's in the backup
	if data.MongoDB != nil {
		systemSteps = append(systemSteps, step("mongodb", "run-app", &bootstrap.RunAppAction{
			ExpandedFormation: data.MongoDB,
		}))
		systemSteps = append(systemSteps, step("mongodb-wait", "wait", &bootstrap.WaitAction{
			URL: "http://mongodb-api.discoverd/ping",
		}))
	}

	state, err := systemSteps.Run(ch, cfg)
	if err != nil {
		return err
	}

	// set DISCOVERD_PEERS in release
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = pg_temp.json_object_update_key(env, 'DISCOVERD_PEERS', '%s')
WHERE release_id = (SELECT release_id FROM apps WHERE name = 'discoverd')
`, state.StepData["discoverd"].(*bootstrap.RunAppState).Release.Env["DISCOVERD_PEERS"]))

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

	var mysqldb io.Reader
	if data.MariaDB != nil {
		mysqldb, err = getFile("mysql.sql.gz")
		if err != nil {
			return err
		}
	}

	// load data into mariadb if it was present in the backup.
	if mysqldb != nil && data.MariaDB != nil {
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

	var mongodb io.Reader
	if data.MongoDB != nil {
		mongodb, err = getFile("mongodb.archive.gz")
		if err != nil {
			return err
		}
	}

	// load data into mongodb if it was present in the backup.
	if mongodb != nil && data.MongoDB != nil {
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

	// get blobstore config
	client, err := controller.NewClient("http://"+controllerInstances[0].Addr, data.Controller.Release.Env["AUTH_KEY"])
	if err != nil {
		return err
	}
	blobstoreRelease, err := client.GetAppRelease("blobstore")
	if err != nil {
		return fmt.Errorf("error getting blobstore release: %s", err)
	}
	blobstoreFormation, err := client.GetExpandedFormation("blobstore", blobstoreRelease.ID)
	if err != nil {
		return fmt.Errorf("error getting blobstore expanded formation: %s", err)
	}
	state.SetControllerKey(data.Controller.Release.Env["AUTH_KEY"])
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
	jsonb := func(m *ct.ImageManifest) []byte {
		data, _ := json.Marshal(m)
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
UPDATE artifacts SET uri = '%s', type = 'flynn', manifest = '%s', layer_url_template = '%s' WHERE artifact_id = (
  SELECT artifact_id FROM release_artifacts WHERE release_id = (
    SELECT release_id FROM apps WHERE name = '%s'
  )
);`, artifact.URI, jsonb(artifact.Manifest), artifact.LayerURLTemplate, step.ID))
	}

	// create the slugbuilder artifact if gitreceive still references it by
	// URI (in which case there is no slugbuilder artifact in the database)
	slugBuilder := artifacts["slugbuilder-image"]
	sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT env->>'SLUGBUILDER_IMAGE_ID' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive')) IS NULL THEN
      INSERT INTO artifacts (artifact_id, type, uri, manifest, layer_url_template) VALUES ('%s', 'flynn', '%s', '%s', '%s');
    END IF;
  END;
$$;`, random.UUID(), slugBuilder.URI, jsonb(slugBuilder.Manifest), slugBuilder.LayerURLTemplate))

	// create the slugrunner artifact if it doesn't exist (which can be the
	// case if no apps were deployed with git push in older clusters where
	// it was created lazily)
	slugRunner := artifacts["slugrunner-image"]
	sqlBuf.WriteString(fmt.Sprintf(`
DO $$
  BEGIN
    IF (SELECT env->>'SLUGRUNNER_IMAGE_ID' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive')) IS NULL THEN
      IF NOT EXISTS (SELECT 1 FROM artifacts WHERE uri = (SELECT env->>'SLUGRUNNER_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive'))) THEN
	INSERT INTO artifacts (artifact_id, type, uri, manifest, layer_url_template) VALUES ('%s', 'flynn', '%s', '%s', '%s');
      END IF;
    END IF;
  END;
$$;`, random.UUID(), slugRunner.URI, jsonb(slugRunner.Manifest), slugRunner.LayerURLTemplate))

	// update slug artifacts currently being referenced by gitreceive
	// (which will also update all current user releases to use the
	// latest slugrunner)
	for _, name := range []string{"slugbuilder", "slugrunner"} {
		artifact := artifacts[name+"-image"]
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%[1]s', type = 'flynn', manifest = '%[2]s', layer_url_template = '%[3]s'
WHERE artifact_id = (SELECT (env->>'%[4]s_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive'))
OR uri = (SELECT env->>'%[4]s_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'gitreceive'));`,
			artifact.URI, jsonb(artifact.Manifest), artifact.LayerURLTemplate, strings.ToUpper(name)))
	}

	// update the URI of redis artifacts currently being referenced by
	// the redis app (which will also update all current redis resources
	// to use the latest redis image)
	sqlBuf.WriteString(fmt.Sprintf(`
UPDATE artifacts SET uri = '%s', type = 'flynn', manifest = '%s', layer_url_template = '%s'
WHERE artifact_id = (SELECT (env->>'REDIS_IMAGE_ID')::uuid FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis'))
OR uri = (SELECT env->>'REDIS_IMAGE_URI' FROM releases WHERE release_id = (SELECT release_id FROM apps WHERE name = 'redis'));`,
		artifacts["redis-image"].URI, jsonb(artifacts["redis-image"].Manifest), artifacts["redis-image"].LayerURLTemplate))

	// ensure the image ID environment variables are set for legacy apps
	// which use image URI variables
	for _, name := range []string{"redis", "slugbuilder", "slugrunner"} {
		sqlBuf.WriteString(fmt.Sprintf(`
UPDATE releases SET env = jsonb_set(env, '{%[1]s_IMAGE_ID}', ('"' || (SELECT artifact_id::text FROM artifacts WHERE uri = '%[2]s') || '"')::jsonb, true)
WHERE env->>'%[1]s_IMAGE_URI' IS NOT NULL;`,
			strings.ToUpper(name), artifacts[name+"-image"].URI))
	}

	// run the artifact migration SQL against the controller database
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

	// determine whether any releases have slugs or docker images which
	// need to be converted to Flynn images
	releases, err := client.ReleaseList()
	if err != nil {
		return fmt.Errorf("error listing releases: %s", err)
	}
	migrateSlugs := false
	migrateDocker := false
	for _, release := range releases {
		if migrateSlugs && migrateDocker {
			break
		}
		if !migrateSlugs && release.IsGitDeploy() && len(release.ArtifactIDs) == 2 {
			artifact, err := client.GetArtifact(release.ArtifactIDs[1])
			if err == nil && artifact.Type == ct.DeprecatedArtifactTypeFile {
				migrateSlugs = true
			}
		}
		if !migrateDocker && release.IsDockerReceiveDeploy() && len(release.ArtifactIDs) == 1 {
			artifact, err := client.GetArtifact(release.ArtifactIDs[0])
			if err == nil && artifact.Type == ct.DeprecatedArtifactTypeDocker {
				migrateDocker = true
			}
		}
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
		out, err = cmd.CombinedOutput()
		if os.Getenv("DEBUG") != "" {
			fmt.Println(string(out))
		}
		if err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error migrating slug artifacts: %s - %q", err, string(out)),
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

		// TODO: this limit is pretty arbitrary, we should determine
		//       how much space the job needs based on size of images
		cmd.Resources.SetDiskLimit(2 * units.GiB)

		out, err = cmd.CombinedOutput()
		if os.Getenv("DEBUG") != "" {
			fmt.Println(string(out))
		}
		if err != nil {
			ch <- &bootstrap.StepInfo{
				StepMeta:  meta,
				State:     "error",
				Error:     fmt.Sprintf("error migrating docker artifacts: %s - %q", err, string(out)),
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
