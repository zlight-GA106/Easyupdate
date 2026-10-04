package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func TestLatestVersionComparisonAndDownload(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	id, _ := s.db.SaveApp(ctx, database.App{Name: "Demo", PackageName: "com.example.demo"})
	data, _ := os.ReadFile("../storage/testdata/helloworld.apk")
	file, err := s.storage.Stage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.storage.Commit(file, id, 164)
	if err != nil {
		t.Fatal(err)
	}
	releaseID, err := s.db.CreateRelease(ctx, database.Release{AppID: id, VersionCode: 164, VersionName: "1.6.4", APKFilename: "demo.apk", APKPath: path, APKSize: file.Size, APKSHA256: file.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	s.db.Publish(ctx, releaseID, true)
	for _, tc := range []struct {
		query  string
		status int
		update bool
	}{{"", 200, true}, {"?version_code=163", 200, true}, {"?version_code=164", 200, false}, {"?version_code=165", 200, false}, {"?version_code=-1", 400, false}, {"?version_code=abc", 400, false}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/apps/com.example.demo/latest"+tc.query, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.query, w.Code)
		}
		if tc.status == 200 {
			var v struct {
				Update bool `json:"update_available"`
			}
			json.Unmarshal(w.Body.Bytes(), &v)
			if v.Update != tc.update {
				t.Fatalf("%s: %+v", tc.query, v)
			}
		}
	}
	download := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/apps/com.example.demo/releases/164/download", nil)
		r.Header.Set("Range", "bytes=0-31")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := download()
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), data[:32]) {
		t.Fatalf("range: %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatal("mime")
	}
	s.db.Publish(ctx, releaseID, false)
	if w = download(); w.Code != 404 {
		t.Fatal("unpublished download available")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/apps/com.example.demo/latest", nil))
	if w.Code != 404 {
		t.Fatal("no_release")
	}
}
