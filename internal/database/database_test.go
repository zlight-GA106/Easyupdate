package database

import (
	"path/filepath"
	"testing"
)

func TestMigrationAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "easyupdate.db")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var version, count int
		if err = s.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
			t.Fatalf("schema: %d, %v", version, err)
		}
		if err = s.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count); err != nil || count != 5 {
			t.Fatalf("tables: %d, %v", count, err)
		}
		s.DB.Close()
	}
}
