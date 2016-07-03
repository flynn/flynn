package updater

import (
	ct "github.com/flynn/flynn/controller/types"
)

type SystemApp struct {
	Name          string
	MinVersion    string          // minimum version this updater binary is capable of updating
	Image         string          // image name if not same as flynn/<name>, ignored if empty
	ImageOnly     bool            // no application, just update the image
	UpdateRelease UpdateReleaseFn // function to migrate changes to release
	Optional      bool            // This system component is optional and may not be present

}

type UpdateReleaseFn func(r *ct.Release)

var SystemApps = []SystemApp{
	{
		Name: "discoverd",
		// versions prior to this one do not have hooks to update
		MinVersion: "v20151129.0",
	},
	{Name: "blobstore"},
	{Name: "taffy"},
	{Name: "dashboard"},
	{Name: "router"},
	{Name: "gitreceive"},
	{Name: "docker-receive"},
	{Name: "controller"},
	{Name: "logaggregator"},
	{
		Name:  "postgres",
		Image: "flynn/postgresql",
		UpdateRelease: func(r *ct.Release) {
			r.Env["SIRENIA_PROCESS"] = "postgres"
		},
	},
	{Name: "status"},
	{Name: "slugbuilder", ImageOnly: true},
	{Name: "slugrunner", ImageOnly: true},
	{Name: "mariadb", Optional: true},
	{Name: "mongodb", Optional: true},
}
