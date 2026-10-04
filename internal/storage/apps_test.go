package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteAppLimitsCleanupToItsDirectoryAndPendingFiles(t *testing.T) {
	local, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []int64{1, 2} {
		for _, version := range []int64{1, 2} {
			path := local.Path(app, version)
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte("APK"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	staged := filepath.Join(local.Root, ".staging", "target.apk")
	keep := filepath.Join(local.Root, ".staging", "other.apk")
	for _, path := range []string{staged, keep} {
		if err = os.WriteFile(path, []byte("staged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = local.DeleteApp(1, Staged{Path: staged}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(local.Root, "1"), staged} {
		if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target remains: %s %v", path, err)
		}
	}
	for _, path := range []string{local.Path(2, 1), local.Path(2, 2), keep} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("unrelated file removed: %s %v", path, err)
		}
	}
	if err = local.DeleteApp(0); err == nil {
		t.Fatal("accepted invalid app id")
	}
	if err = local.DeleteApp(-1); err == nil {
		t.Fatal("accepted negative app id")
	}
	if err = local.DeleteApp(1); err != nil {
		t.Fatalf("already missing directory: %v", err)
	}
}

func TestDeleteAppRejectsPendingPathsOutsideStaging(t *testing.T) {
	local, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	keep := local.Path(2, 1)
	if err = os.MkdirAll(filepath.Dir(keep), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keep, []byte("other APK"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = local.DeleteApp(1, Staged{Path: keep}); err == nil {
		t.Fatal("accepted cross-app pending path")
	}
	if _, err = os.Stat(keep); err != nil {
		t.Fatalf("other app file touched: %v", err)
	}
}

func TestDeleteAppDoesNotFollowDirectorySymlink(t *testing.T) {
	local, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.apk")
	if err = os.WriteFile(keep, []byte("outside storage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(local.Root, "1")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err = local.DeleteApp(1); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(keep); err != nil {
		t.Fatalf("symlink target touched: %v", err)
	}
}
