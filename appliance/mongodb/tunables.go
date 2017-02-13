package mongodb

var allowedTunables = map[string]bool{
	"storage.wiredTiger.engineConfig.cacheSizeGB": true,
}
