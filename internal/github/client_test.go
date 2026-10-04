package github

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPublicRepositoryAndTokenBoundary(t *testing.T) {
	c := New("test-secret")
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"private":false}`
		if r.URL.Hostname() == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer test-secret" {
				t.Fatal("missing API token")
			}
			if strings.HasSuffix(r.URL.Path, "/releases") {
				body = `[{"id":1,"tag_name":"v1","published_at":"2026-10-01T00:00:00Z","assets":[]}]`
			}
		} else {
			if r.Header.Get("Authorization") != "" {
				t.Fatal("token leaked to asset host")
			}
			body = "apk"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := c.Releases(context.Background(), "owner", "repo")
	if err != nil || len(items) != 1 {
		t.Fatalf("releases: %v", err)
	}
	body, err := c.Download(context.Background(), "https://github.com/owner/repo/releases/download/v1/app.apk")
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"private":true}`)), Header: make(http.Header)}, nil
	})
	if _, err = c.Releases(context.Background(), "owner", "repo"); err == nil {
		t.Fatal("private repository accepted")
	}
}
func TestRejectUnsafeAssetURLs(t *testing.T) {
	for _, raw := range []string{"http://github.com/a.apk", "https://127.0.0.1/a.apk", "https://github.com:8080/a.apk", "https://user:pass@github.com/a.apk", "https://example.com/a.apk"} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal(raw)
		}
	}
}
