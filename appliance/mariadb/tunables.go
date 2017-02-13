package mariadb

type mariaTunable struct {
	Static  bool
	Default string
}

// All allowed MariaDB tunables and whether they require a restart or not
var allowedTunables = map[string]mariaTunable{
	"innodb_doublewrite":           {true, "1"},
	"innodb_buffer_pool_instances": {true, "8"},
	"skip_name_resolve":            {true, "0"},
	"max_heap_table_size":          {false, "16777216"},
	"query_cache_limit":            {false, "1048576"},
	"tmp_table_size":               {false, "33554432"}, // Debian default 32mb vs 16mb source distribution
}
