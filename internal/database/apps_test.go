package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}
func TestAppLookupAndUniquePackage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, err := s.SaveApp(ctx, App{Name: "EasyCent", PackageName: "com.zlight106.easycent"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.AppByPackage(ctx, "com.zlight106.easycent")
	if err != nil || a.ID != id {
		t.Fatalf("lookup: %+v %v", a, err)
	}
	if _, err = s.SaveApp(ctx, App{Name: "Duplicate", PackageName: a.PackageName}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	a.Name = "Renamed"
	if _, err = s.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	items, err := s.Apps(ctx)
	if err != nil || len(items) != 1 || items[0].Name != "Renamed" {
		t.Fatalf("list: %+v %v", items, err)
	}
	if err = s.DeleteApp(ctx, id); err != nil {
		t.Fatal(err)
	}
}
