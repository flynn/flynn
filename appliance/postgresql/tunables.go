package postgresql

// All allowed Postgres tunables and whether they require a restart or not
var allowedTunables = map[string]bool{
	"dynamic_shared_memory_type":   true,
	"shared_buffers":               true,
	"max_wal_senders":              true,
	"wal_keep_segments":            false,
	"max_standby_archive_delay":    false,
	"max_standby_streaming_delay":  false,
	"wal_receiver_status_interval": false,
	"datestyle":                    false,
	"timezone":                     false,
	"client_encoding":              false,
	"log_line_prefix":              false,
	"log_timezone":                 false,
	"log_min_messages":             false,
	"log_connections":              false,
	"log_disconnections":           false,
	"default_text_search_config":   false,
	"local_preload_libraries":      false,
	"extwlist.extensions":          false,
	"work_mem":                     false,
	"effective_cache_size":         false,
	"checkpoint_completion_target": false,
	"maintenance_work_mem":         false,
	"default_statistics_target":    false,
}
