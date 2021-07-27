module github.com/flynn/flynn

go 1.13

require (
	cloud.google.com/go v0.43.0
	github.com/Azure/azure-sdk-for-go v0.0.0-20160912221952-63d3f3e3b12f
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78
	github.com/BurntSushi/toml v0.3.1
	github.com/armon/go-metrics v0.0.0-20150601112433-b2d95e5291cd // indirect
	github.com/aws/aws-sdk-go v0.0.0-20170816181422-2063d937ea69
	github.com/boltdb/bolt v1.3.1
	github.com/checkpoint-restore/go-criu v0.0.0-20181120144056-17b0214f6c48 // indirect
	github.com/cheggaaa/pb v0.0.0-20150223212723-0464652af750
	github.com/containerd/console v0.0.0-20181022165439-0650fd9eeb50 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/cupcake/jsonschema v0.0.0-20160618151340-51bf6945446b
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/dgryski/go-skip32 v0.0.0-20131221203938-6cc5a8b574de
	github.com/docker/go-units v0.3.0
	github.com/dustin/go-humanize v1.0.0
	github.com/flynn/go-check v0.0.0-20150613200214-592122021381
	github.com/flynn/go-docopt v0.0.0-20140912013429-f6dd2ebbb31e
	github.com/flynn/go-p9p v0.0.0-20170717161903-42f7901ca21a
	github.com/flynn/go-tuf v0.0.0-20190425212541-cf1ac7de1ebf
	github.com/flynn/que-go v0.0.0-20150926162331-737f00726577
	github.com/flynn/tail v0.0.0-20180226200612-fc12669dc660
	github.com/garyburd/redigo v0.0.0-20151219232044-836b6e58b335
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/go-ini/ini v1.12.0 // indirect
	github.com/go-sql-driver/mysql v0.0.0-20160125151823-7c7f55628262
	github.com/go-stack/stack v1.7.0 // indirect
	github.com/godbus/dbus v4.1.0+incompatible // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/groupcache v0.0.0-20160803200408-a6b377e3400b
	github.com/golang/protobuf v1.4.1
	github.com/google/go-cmp v0.4.0
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0
	github.com/hashicorp/go-msgpack v0.0.0-20150518234257-fa3f63826f7c // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/hashicorp/raft v0.0.0-20160603202243-4bcac2adb069
	github.com/hashicorp/raft-boltdb v0.0.0-20150201200839-d1e82c1ec3f1
	github.com/howeyc/fsnotify v0.0.0-20140711012604-6b1ef893dc11 // indirect
	github.com/improbable-eng/grpc-web v0.11.0
	github.com/inconshreveable/log15 v0.0.0-20171019012758-0decfc6c20d9
	github.com/jackc/fake v0.0.0-20150926172116-812a484cc733 // indirect
	github.com/jackc/pgx v0.0.0-20160715195140-558d5550cf5c
	github.com/jmespath/go-jmespath v0.0.0-20160202185014-0b12d6b521d8 // indirect
	github.com/jtacoma/uritemplates v1.0.0
	github.com/julienschmidt/httprouter v0.0.0-20140925104356-46807412fe50
	github.com/kardianos/osext v0.0.0-20150223151934-ccfcd0245381
	github.com/kavu/go_reuseport v1.4.0
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kr/binarydist v0.0.0-20120828065244-9955b0ab8708 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/kr/pty v1.1.8
	github.com/krolaw/dhcp4 v0.0.0-20180925202202-7cead472c414
	github.com/kylelemons/godebug v0.0.0-20131002215753-808ac284003c
	github.com/mattn/go-colorable v0.0.0-20140924234614-043ae1629135
	github.com/mattn/go-isatty v0.0.0-20151211000621-56b76bdf51f7 // indirect
	github.com/miekg/dns v0.0.0-20160726032027-db96a2b759cd
	github.com/minio/minio-go v0.0.0-20170324230031-29b05151452a
	github.com/mistifyio/go-zfs v0.0.0-20141209150540-dda1f4cd04dc
	github.com/mitchellh/go-homedir v0.0.0-20140913165950-7d2d8c8a4e07
	github.com/mrunalp/fileutils v0.0.0-20171103030105-7d4729fb3618 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
	github.com/olekukonko/ts v0.0.0-20140412220145-ecf753e7c962 // indirect
	github.com/opencontainers/runc v1.0.0-rc95
	github.com/opencontainers/runtime-spec v1.0.1 // indirect
	github.com/opencontainers/selinux v1.2.2 // indirect
	github.com/pkg/errors v0.8.1
	github.com/rancher/sparse-tools v0.0.0-20190307223929-666f9b3bde21
	github.com/rnd42/go-jsonpointer v0.0.0-20140520035338-0480215403db // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/seccomp/libseccomp-golang v0.0.0-20160531183505-32f571b70023 // indirect
	github.com/shopspring/decimal v0.0.0-20180709203117-cd690d0c9e24 // indirect
	github.com/smartystreets/goconvey v0.0.0-20190710185942-9d28bd7c0945 // indirect
	github.com/stevvooe/resumable v0.0.0-20150521211217-51ad44105773
	github.com/syndtr/gocapability v0.0.0-20180916011248-d98352740cb2 // indirect
	github.com/tent/canonical-json-go v0.0.0-20130607151641-96e4ba3a7613
	github.com/vishvananda/netlink v0.0.0-20170502164845-1e045880fbc2
	github.com/vishvananda/netns v0.0.0-20170219233438-54f0e4339ce7 // indirect
	golang.org/x/crypto v0.0.0-20190911031432-227b76d455e7
	golang.org/x/net v0.0.0-20190918130420-a8b05e9114ab
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sys v0.0.0-20190804053845-51ab0e2deafa
	google.golang.org/api v0.7.0
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013
	google.golang.org/grpc v1.27.0
	google.golang.org/protobuf v1.24.0
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/inconshreveable/go-update.v0 v0.0.0-20150814200126-d8b0b1d421aa
	gopkg.in/inconshreveable/log15.v2 v2.0.0-20180818164646-67afb5ed74ec // indirect
	gopkg.in/mgo.v2 v2.0.0-20160609180028-29cc868a5ca6
	gopkg.in/natefinch/lumberjack.v2 v2.0.0-20151013014448-600ceb4523e5
	gopkg.in/tomb.v1 v1.0.0-20140529071818-c131134a1947 // indirect
	gopkg.in/vmihailenco/msgpack.v2 v2.9.1 // indirect
	gopkg.in/yaml.v2 v2.2.2
	gotest.tools v0.0.0-20181223230014-1083505acf35
	labix.org/v2/mgo v0.0.0-20140701140051-000000000287 // indirect
	launchpad.net/gocheck v0.0.0-20140225173054-000000000087 // indirect
)

replace github.com/opencontainers/runc => github.com/flynn/runc v1.0.0-rc1001

replace github.com/godbus/dbus => github.com/godbus/dbus/v5 v5.0.2

replace github.com/coreos/pkg => github.com/flynn/coreos-pkg v1.0.1
