package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestLatestUsesPublishedVersionCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	appID, _ := s.SaveApp(ctx, App{Name: "Test", PackageName: "com.example.test"})
	if _, err := s.Latest(ctx, appID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unexpected latest")
	}
	makeRelease := func(code int64, pub bool) int64 {
		id, err := s.CreateRelease(ctx, Release{AppID: appID, VersionName: "Test", VersionCode: code, APKFilename: "test.apk", APKPath: "app.apk", APKSHA256: "abc", APKSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		if pub {
			s.Publish(ctx, id, true)
		}
		return id
	}
	high := makeRelease(164, true)
	makeRelease(999, false)
	makeRelease(163, true)
	latest, err := s.Latest(ctx, appID)
	if err != nil || latest.ID != high {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if err = s.Publish(ctx, high, false); err != nil {
		t.Fatal(err)
	}
	latest, _ = s.Latest(ctx, appID)
	if latest.VersionCode != 163 {
		t.Fatal("unpublish ignored")
	}
	if err = s.DeleteApp(ctx, appID); err != nil {
		t.Fatal(err)
	}
	if releases, err := s.Releases(ctx, appID); err != nil || len(releases) != 0 {
		t.Fatalf("deleted app releases: %+v %v", releases, err)
	}
}
