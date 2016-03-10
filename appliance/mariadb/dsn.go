package mariadb

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DSN returns a URL-formatted data source name.
type DSN struct {
	Host     string
	User     string
	Password string
	Database string
	Timeout  time.Duration
}

// String encodes dsn to a URL string format.
func (dsn *DSN) String() string {
	u := url.URL{
		Host: fmt.Sprintf("tcp(%s)", dsn.Host),
		Path: "/" + dsn.Database,
		RawQuery: url.Values{
			"timeout": {dsn.Timeout.String()},
		}.Encode(),
	}

	// Set password, if available.
	if dsn.Password == "" {
		u.User = url.User(dsn.User)
	} else {
		u.User = url.UserPassword(dsn.User, dsn.Password)
	}

	// Remove leading double-slash.
	return strings.TrimPrefix(u.String(), "//")
}
