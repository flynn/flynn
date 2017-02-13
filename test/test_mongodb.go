package main

import (
	"fmt"
	"path/filepath"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	c "github.com/flynn/go-check"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type MongoDBSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&MongoDBSuite{})

type mgoLogger struct {
	t *c.C
}

func (l mgoLogger) Output(calldepth int, s string) error {
	debugf(l.t, s)
	return nil
}

func (s *MongoDBSuite) TestDumpRestore(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	res := r.flynn("resource", "add", "mongodb")
	t.Assert(res, Succeeds)
	id := strings.Split(res.Output, " ")[2]

	// dumping an empty database should not fail
	file := filepath.Join(t.MkDir(), "db.dump")
	t.Assert(r.flynn("mongodb", "dump", "-f", file), Succeeds)

	t.Assert(r.flynn("mongodb", "mongo", "--", "--eval", `db.foos.insert({data: "foobar"})`), Succeeds)

	t.Assert(r.flynn("mongodb", "dump", "-f", file), Succeeds)
	t.Assert(r.flynn("mongodb", "mongo", "--", "--eval", "db.foos.drop()"), Succeeds)

	r.flynn("mongodb", "restore", "-f", file)
	query := r.flynn("mongodb", "mongo", "--", "--eval", "db.foos.find()")
	t.Assert(query, SuccessfulOutputContains, "foobar")

	t.Assert(r.flynn("resource", "remove", "mongodb", id), Succeeds)
}

// Sirenia integration tests
var sireniaMongoDB = sireniaDatabase{
	appName:    "mongodb",
	serviceKey: "FLYNN_MONGO",
	hostKey:    "MONGO_HOST",
	assertWriteable: func(t *c.C, r *ct.Release, d *sireniaFormation) {
		mgo.SetLogger(mgoLogger{t})
		mgo.SetDebug(true)
		session, err := mgo.DialWithInfo(&mgo.DialInfo{
			Addrs:    []string{fmt.Sprintf("leader.%s.discoverd", d.name)},
			Username: "flynn",
			Password: r.Env["MONGO_PWD"],
			Database: "admin",
			Direct:   true,
		})
		session.SetMode(mgo.Monotonic, true)
		defer session.Close()
		t.Assert(err, c.IsNil)
		t.Assert(session.DB("test").C("test").Insert(&bson.M{"test": "test"}), c.IsNil)
	},
}

func (s *MongoDBSuite) TestDeploySingleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mongodb-single-async",
		db:          sireniaMongoDB,
		sireniaJobs: 3,
		webJobs:     2,
	}, testDeploySingleAsync)
}

func (s *MongoDBSuite) TestDeployMultipleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mongodb-multiple-async",
		db:          sireniaMongoDB,
		sireniaJobs: 5,
		webJobs:     2,
	}, testDeployMultipleAsync)
}

func (s *MongoDBSuite) TestTunables(t *c.C) {
	testSireniaTunables(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mongodb-tunables",
		db:          sireniaMongoDB,
		sireniaJobs: 3,
		webJobs:     2,
	}, []tunableTest{
		{"requires restart", sireniaTunable{"storage.wiredTiger.engineConfig.cacheSizeGB", "1", "2"}},
	})
}
