package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"github.com/zlight-GA106/EasyUpdate/internal/storage"
)

func appDeletionSession(s *Server) (*http.Cookie, string) {
	w := httptest.NewRecorder()
	session := s.auth.create(w, httptest.NewRequest("GET", "/", nil), true)
	return w.Result().Cookies()[0], session.CSRF
}

func appDeletionRequest(s *Server, cookie *http.Cookie, method, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func appDeletionFixture(t *testing.T, s *Server, pkg string) (database.App, database.Release, storage.Staged) {
	t.Helper()
	ctx := context.Background()
	id, err := s.db.SaveApp(ctx, database.App{Name: pkg, PackageName: pkg})
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.db.App(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../storage/testdata/helloworld.apk")
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.storage.Stage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.storage.Commit(file, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	releaseID, err := s.db.CreateRelease(ctx, database.Release{AppID: id, VersionName: "1.0", VersionCode: 1, APKFilename: "app.apk", APKPath: path, APKSize: file.Size, APKSHA256: file.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.Publish(ctx, releaseID, true); err != nil {
		t.Fatal(err)
	}
	release, err := s.db.Release(ctx, releaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.Heartbeat(ctx, database.Device{AppID: id, DeviceID: "shared-device", VersionCode: 1, VersionName: "1.0"}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.SaveGitHubSource(ctx, database.GitHubSource{AppID: id, Owner: "example", Repo: "demo", Enabled: true, AssetPattern: "*.apk"}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.storage.Stage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return app, release, pending
}

func TestAppDeletionRequiresConfirmationAndCleansOnlyTarget(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	target, targetRelease, targetPending := appDeletionFixture(t, s, "com.example.target")
	other, otherRelease, otherPending := appDeletionFixture(t, s, "com.example.other")
	s.pending["target-token"] = pendingUpload{AppID: target.ID, File: targetPending}
	s.pending["other-token"] = pendingUpload{AppID: other.ID, File: otherPending}
	cookie, csrf := appDeletionSession(s)
	path := fmt.Sprintf("/admin/apps/%d/delete", target.ID)
	w := appDeletionRequest(s, cookie, "GET", path, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), target.PackageName) || !strings.Contains(w.Body.String(), "1 个版本") || !strings.Contains(w.Body.String(), "1 条设备记录") {
		t.Fatalf("confirmation page: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		confirmation, csrf string
		status             int
	}{{"wrong", csrf, 400}, {target.PackageName, "wrong-csrf", 403}} {
		w = appDeletionRequest(s, cookie, "POST", path, url.Values{"confirmation": {tc.confirmation}, "csrf": {tc.csrf}})
		if w.Code != tc.status {
			t.Fatalf("rejected deletion: %d, want %d", w.Code, tc.status)
		}
		if _, err := s.db.App(ctx, target.ID); err != nil {
			t.Fatalf("app changed before confirmation: %v", err)
		}
		if _, err := os.Stat(targetRelease.APKPath); err != nil {
			t.Fatalf("APK changed before confirmation: %v", err)
		}
		if len(s.pending) != 2 {
			t.Fatal("pending uploads changed before confirmation")
		}
	}
	w = appDeletionRequest(s, cookie, "POST", path, url.Values{"confirmation": {target.PackageName}, "csrf": {csrf}})
	if w.Code != 303 || w.Header().Get("Location") != "/admin/apps" {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.db.App(ctx, target.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted app: %v", err)
	}
	if _, err := s.db.Release(ctx, targetRelease.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted release: %v", err)
	}
	if devices, err := s.db.Devices(ctx, target.ID, 10, 0); err != nil || len(devices) != 0 {
		t.Fatalf("deleted devices: %+v %v", devices, err)
	}
	if _, err := s.db.GitHubSource(ctx, target.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted GitHub source: %v", err)
	}
	if _, err := s.db.App(ctx, other.ID); err != nil {
		t.Fatalf("other app: %v", err)
	}
	if _, err := s.db.Release(ctx, otherRelease.ID); err != nil {
		t.Fatalf("other release: %v", err)
	}
	for _, path := range []string{filepath.Join(s.storage.Root, fmt.Sprint(target.ID)), targetPending.Path} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target file remains: %s %v", path, err)
		}
	}
	for _, path := range []string{otherRelease.APKPath, otherPending.Path} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file removed: %s %v", path, err)
		}
	}
	if _, exists := s.pending["target-token"]; exists {
		t.Fatal("deleted application's token remains")
	}
	if _, exists := s.pending["other-token"]; !exists {
		t.Fatal("other application's token removed")
	}
	for _, endpoint := range []string{"/api/v1/apps/" + target.PackageName + "/latest", downloadRoute(target.PackageName, 1)} {
		w = appDeletionRequest(s, nil, "GET", endpoint, nil)
		if w.Code != 404 {
			t.Fatalf("deleted app remains public at %s: %d", endpoint, w.Code)
		}
	}
}

func TestAppDeletionInvalidatesDatabaseAndPendingOnCleanupFailure(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	app, _, staged := appDeletionFixture(t, s, "com.example.target")
	s.storage.Discard(staged)
	outside := filepath.Join(t.TempDir(), "outside.apk")
	if err := os.WriteFile(outside, []byte("leave untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	// A malformed pending path must fail safely instead of deleting this file.
	s.pending["bad-path"] = pendingUpload{AppID: app.ID, File: storage.Staged{Path: outside}}
	cookie, csrf := appDeletionSession(s)
	w := appDeletionRequest(s, cookie, "POST", fmt.Sprintf("/admin/apps/%d/delete", app.ID), url.Values{"confirmation": {app.PackageName}, "csrf": {csrf}})
	if w.Code != 500 || !strings.Contains(w.Body.String(), "应用及关联记录已删除，但文件清理失败") {
		t.Fatalf("cleanup error: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.db.App(ctx, app.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("app remained active after failed file cleanup: %v", err)
	}
	if len(s.pending) != 0 {
		t.Fatal("deleted application's pending token remained active")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "leave untouched" {
		t.Fatalf("outside file changed: %q %v", data, err)
	}
	w = appDeletionRequest(s, nil, "GET", downloadRoute(app.PackageName, 1), nil)
	if w.Code != 404 {
		t.Fatalf("deleted APK still offered after cleanup error: %d", w.Code)
	}
}

func TestPendingUploadCannotAttachAfterAppDeletionOrIDReuse(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		t.Run(fmt.Sprint("recreate=", recreate), func(t *testing.T) {
			s := testServer(t)
			ctx := context.Background()
			app, _, staged := appDeletionFixture(t, s, "com.example.helloworld")
			if err := s.db.DeleteApp(ctx, app.ID); err != nil {
				t.Fatal(err)
			}
			wantStatus := 404
			if recreate {
				id, err := s.db.SaveApp(ctx, database.App{Name: "Replacement", PackageName: app.PackageName})
				if err != nil || id != app.ID {
					t.Fatalf("expected reused ID: %d %v", id, err)
				}
				wantStatus = 409
			}
			cookie, _ := appDeletionSession(s)
			r := httptest.NewRequest("POST", "/admin/apps/1/upload", nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			s.showConfirmation(w, r, app, pendingUpload{File: staged, AppID: app.ID, Expires: time.Now().Add(time.Minute)})
			if w.Code != wantStatus {
				t.Fatalf("stale staged upload: %d, want %d: %s", w.Code, wantStatus, w.Body.String())
			}
			if len(s.pending) != 0 {
				t.Fatal("stale staged upload became pending")
			}
			if _, err := os.Stat(staged.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale staged APK not discarded: %v", err)
			}
		})
	}
}
