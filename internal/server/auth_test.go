package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zlight-GA106/EasyUpdate/internal/config"
	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	c := config.Config{}
	c.Admin.Username = "admin"
	c.Admin.Password = "admin"
	c.Server.PublicURL = "http://example.test"
	c.Storage.Path = filepath.Join(t.TempDir(), "apks")
	c.Storage.MaxUploadMB = 1
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	s, err := New(c, db, os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([a-f0-9]+)"`)

func TestLoginCSRFAndSession(t *testing.T) {
	s := testServer(t)
	get := httptest.NewRecorder()
	s.ServeHTTP(get, httptest.NewRequest("GET", "/login", nil))
	token := csrfPattern.FindStringSubmatch(get.Body.String())
	if len(token) != 2 {
		t.Fatal("missing csrf")
	}
	cookie := get.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags")
	}
	login := func(csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"username": {"admin"}, "password": {"admin"}, "csrf": {csrf}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := login("bad"); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	w := login(token[1])
	if w.Code != 303 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	authCookie := w.Result().Cookies()[0]
	if authCookie.Value == cookie.Value {
		t.Fatal("session not rotated")
	}
	r := httptest.NewRequest("GET", "/admin", nil)
	r.AddCookie(authCookie)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "概览") {
		t.Fatalf("admin: %d", w.Code)
	}
	req := httptest.NewRequest("POST", "/admin/apps/new", strings.NewReader("name=Bad&package_name=bad"))
	req.AddCookie(authCookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("admin csrf: %d", w.Code)
	}
	anonymous := httptest.NewRecorder()
	s.ServeHTTP(anonymous, httptest.NewRequest("GET", "/admin", nil))
	if anonymous.Code != 303 {
		t.Fatal("anonymous access")
	}
}
func TestLoginRateLimit(t *testing.T) {
	s := testServer(t)
	get := httptest.NewRecorder()
	s.ServeHTTP(get, httptest.NewRequest("GET", "/login", nil))
	token := csrfPattern.FindStringSubmatch(get.Body.String())[1]
	cookie := get.Result().Cookies()[0]
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"username": {"admin"}, "password": {"bad"}, "csrf": {token}}.Encode()))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 401
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
		io.Copy(io.Discard, w.Result().Body)
	}
}
