package client

import (
	"database/sql"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t)
}
