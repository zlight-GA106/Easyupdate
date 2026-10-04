package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func TestDeleteAppRemovesAssociatedRecordsOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var ids []int64
	for _, name := range []string{"target", "keep"} {
		id, err := s.SaveApp(ctx, App{Name: name, PackageName: "com.example." + name})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		for _, code := range []int64{1, 2} {
			if _, err = s.CreateRelease(ctx, Release{AppID: id, VersionName: "1.0", VersionCode: code, APKFilename: "app.apk", APKPath: "app.apk", APKSize: 1, APKSHA256: "abc"}); err != nil {
				t.Fatal(err)
			}
		}
		if err = s.Heartbeat(ctx, Device{AppID: id, DeviceID: "shared-device", VersionName: "1.0", VersionCode: 1}); err != nil {
			t.Fatal(err)
		}
		if err = s.SaveGitHubSource(ctx, GitHubSource{AppID: id, Owner: "example", Repo: name, Enabled: true, AssetPattern: "*.apk"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteApp(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.App(ctx, ids[0]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted app: %v", err)
	}
	if _, err := s.App(ctx, ids[1]); err != nil {
		t.Fatalf("other app: %v", err)
	}
	for _, table := range []string{"releases", "devices", "github_sources"} {
		for i, id := range ids {
			var count int
			if err := s.DB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE app_id=?", table), id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if i == 1 {
				want = 1
				if table == "releases" {
					want = 2
				}
			}
			if count != want {
				t.Fatalf("%s app %d: %d, want %d", table, id, count, want)
			}
		}
	}
	if err := s.DeleteApp(ctx, ids[0]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing app: %v", err)
	}
}

func TestDeleteAppRollsBackAssociatedRecordsOnFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, err := s.SaveApp(ctx, App{Name: "Target", PackageName: "com.example.target"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.CreateRelease(ctx, Release{AppID: id, VersionName: "1.0", VersionCode: 1, APKFilename: "app.apk", APKPath: "app.apk", APKSize: 1, APKSHA256: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `CREATE TRIGGER reject_app_deletion BEFORE DELETE ON apps BEGIN SELECT RAISE(ABORT,'deletion blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteApp(ctx, id); err == nil {
		t.Fatal("deletion unexpectedly succeeded")
	}
	if _, err = s.App(ctx, id); err != nil {
		t.Fatalf("app was removed despite failure: %v", err)
	}
	if _, err = s.Release(ctx, release); err != nil {
		t.Fatalf("release was removed despite rollback: %v", err)
	}
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
