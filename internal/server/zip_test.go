package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func TestZIPUploadPublishAndDownload(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	appID, _ := s.db.SaveApp(ctx, database.App{Name: "Desktop", PackageName: "com.example.desktop"})
	var artifact bytes.Buffer
	z := zip.NewWriter(&artifact)
	m, _ := z.Create("easyupdate.json")
	m.Write([]byte(`{"package_name":"com.example.desktop","version_name":"1.7.0","version_code":10700}`))
	f, _ := z.Create("Demo.exe")
	f.Write([]byte("desktop application"))
	z.Close()
	auth := httptest.NewRecorder()
	sess := s.auth.create(auth, httptest.NewRequest("GET", "/", nil), true)
	cookie := auth.Result().Cookies()[0]
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf", sess.CSRF)
	part, _ := mw.CreateFormFile("apk", "desktop.zip")
	part.Write(artifact.Bytes())
	mw.Close()
	r := httptest.NewRequest("POST", "/admin/apps/1/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var token string
	for key := range s.pending {
		token = key
	}
	form := url.Values{"csrf": {sess.CSRF}, "upload_token": {token}, "package_name": {"com.example.desktop"}, "version_name": {"1.7.0"}, "version_code": {"10700"}, "action": {"create"}}
	post := func(route string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", route, strings.NewReader(values.Encode()))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	form.Set("version_code", "10701")
	if w = post("/admin/apps/1/releases", form); w.Code != 400 {
		t.Fatal("metadata override accepted")
	}
	form.Set("version_code", "10700")
	if w = post("/admin/apps/1/releases", form); w.Code != 303 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	rel, err := s.db.ReleaseByCode(ctx, appID, 10700)
	if err != nil || rel.ArtifactType() != "zip" {
		t.Fatalf("release: %+v %v", rel, err)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/apps/com.example.desktop/releases/10700/download", nil))
	if w.Code != 404 {
		t.Fatal("draft publicly accessible")
	}
	if w = post("/admin/releases/1/publish", url.Values{"csrf": {sess.CSRF}, "action": {"publish"}}); w.Code != 303 {
		t.Fatal("publish failed")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/apps/com.example.desktop/latest?version_code=10603", nil))
	var latest map[string]any
	json.Unmarshal(w.Body.Bytes(), &latest)
	if w.Code != 200 || latest["artifact_type"] != "zip" || latest["file_name"] != "desktop.zip" || latest["update_available"] != true {
		t.Fatalf("latest: %s", w.Body.String())
	}
	r = httptest.NewRequest("GET", "/api/v1/apps/com.example.desktop/releases/10700/download", nil)
	r.Header.Set("Range", "bytes=0-31")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 206 || w.Header().Get("Content-Type") != "application/zip" || !bytes.Equal(w.Body.Bytes(), artifact.Bytes()[:32]) {
		t.Fatalf("range: %d %s", w.Code, w.Body.String())
	}
}
