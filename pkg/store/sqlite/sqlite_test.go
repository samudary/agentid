package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/samudary/agentid/pkg/store/sqlite"
	"github.com/samudary/agentid/pkg/store/storetest"
)

func TestSQLiteStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer s.Close()

	storetest.TestStore(t, s)
}
