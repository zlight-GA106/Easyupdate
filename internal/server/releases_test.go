package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func TestPackageMismatchRejected(t *testing.T) {
	s := testServer(t)
	id, _ := s.db.SaveApp(context.Background(), database.App{Name: "Wrong", PackageName: "com.example.other"})
	data, err := os.ReadFile("../storage/testdata/helloworld.apk")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	authResponse := httptest.NewRecorder()
	sess := s.auth.create(authResponse, req, true)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf", sess.CSRF)
	part, _ := mw.CreateFormFile("apk", "test.apk")
	part.Write(data)
	mw.Close()
	req = httptest.NewRequest("POST", "/admin/apps/1/upload", &body)
	req.AddCookie(authResponse.Result().Cookies()[0])
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if id != 1 || w.Code != 400 {
		t.Fatalf("mismatch: %d %s", w.Code, w.Body.String())
	}
	releases, _ := s.db.Releases(context.Background(), id)
	if len(releases) != 0 {
		t.Fatal("release created despite mismatch")
	}
}
