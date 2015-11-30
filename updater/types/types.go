package updater

type SystemApp struct {
	Name       string
	MinVersion string // minimum version this updater binary is capable of updating
}

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
	{Name: "controller"},
	{Name: "logaggregator"},
	{Name: "postgres"},
	{Name: "status"},
}
