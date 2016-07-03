package mongodb

// Config structures

type replSetMember struct {
	ID       int    `bson:"_id"`
	Host     string `bson:"host"`
	Priority int    `bson:"priority"`
	Hidden   bool   `bson:"hidden"`
}

type replSetConfig struct {
	ID      string          `bson:"_id"`
	Members []replSetMember `bson:"members"`
	Version int
}

// Status structures

type replicaState int

const (
	Startup replicaState = iota
	Primary
	Secondary
	Recovering
	Startup2
	Unknown
	Arbiter
	Down
	Rollback
	Removed
)

type replSetOptime struct {
	Timestamp int64 `bson:"ts"`
	Term      int64 `bson:"t"`
}

type replSetStatusMember struct {
	Name      string        `bson:"name"`
	Optime    replSetOptime `bson:"optime"`
	SyncingTo string        `bson:"syncingTo"`
	State     replicaState  `bson:"state"`
}

type replSetStatus struct {
	MyState replicaState          `bson:"myState"`
	Members []replSetStatusMember `bson:"members"`
}
