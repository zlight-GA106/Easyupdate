package server

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zlight-GA106/EasyUpdate/internal/config"
	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"github.com/zlight-GA106/EasyUpdate/internal/storage"
)

type Server struct {
	cfg       config.Config
	db        *database.Store
	templates *template.Template
	auth      *auth
	mux       *http.ServeMux
	storage   *storage.Local
	uploadMu  sync.Mutex
	pending   map[string]pendingUpload
}

func New(c config.Config, db *database.Store, assets fs.FS) (*Server, error) {
	a, err := newAuth(c.Admin.Username, c.Admin.Password, strings.HasPrefix(c.Server.PublicURL, "https://"))
	if err != nil {
		return nil, err
	}
	c.Admin.Password = ""
	funcs := template.FuncMap{"size": formatSize, "date": formatDate, "initial": func(s string) string {
		r := []rune(s)
		if len(r) == 0 {
			return "A"
		}
		return strings.ToUpper(string(r[0]))
	}}
	t, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	local, err := storage.New(c.Storage.Path, c.Storage.MaxUploadMB<<20)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	s := &Server{cfg: c, db: db, templates: t, auth: a, mux: http.NewServeMux(), storage: local, pending: map[string]pendingUpload{}}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin", http.StatusSeeOther) })
	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.HandleFunc("POST /login", s.login)
	s.admin("POST /logout", s.logout)
	s.admin("GET /admin", s.overview)
	s.admin("GET /admin/apps", s.apps)
	s.admin("GET /admin/apps/new", s.appForm)
	s.admin("POST /admin/apps/new", s.saveApp)
	s.admin("GET /admin/apps/{id}", s.appDetail)
	s.admin("GET /admin/apps/{id}/edit", s.appForm)
	s.admin("POST /admin/apps/{id}/edit", s.saveApp)
	s.admin("GET /admin/apps/{id}/delete", s.deleteAppPage)
	s.admin("POST /admin/apps/{id}/delete", s.deleteApp)
	s.admin("GET /admin/settings", s.settings)
	s.admin("GET /admin/apps/{id}/upload", s.uploadPage)
	s.admin("POST /admin/apps/{id}/upload", s.upload)
	s.admin("POST /admin/apps/{id}/releases", s.createRelease)
	s.admin("GET /admin/releases/{id}", s.releasePage)
	s.admin("POST /admin/releases/{id}/edit", s.editRelease)
	s.admin("POST /admin/releases/{id}/publish", s.publishRelease)
	s.admin("GET /admin/releases/{id}/delete", s.deleteReleasePage)
	s.admin("POST /admin/releases/{id}/delete", s.deleteRelease)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/admin") || r.URL.Path == "/login" {
		w.Header().Set("Cache-Control", "no-store")
	}
	defer func() {
		if v := recover(); v != nil {
			slog.Error("request panic", "method", r.Method, "route", r.Pattern, "error", fmt.Sprint(v))
			http.Error(w, "Internal server error", 500)
		}
	}()
	s.mux.ServeHTTP(w, r)
}

func (s *Server) admin(pattern string, fn http.HandlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.auth.current(r)
		if !ok || !sess.LoggedIn {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if err := s.parseForm(w, r); err != nil {
				s.problem(w, r, 400, "请求内容无效或文件过大")
				return
			}
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
			if !s.auth.validCSRF(sess, r.FormValue("csrf")) {
				s.problem(w, r, 403, "会话校验失败，请刷新页面")
				return
			}
		}
		fn(w, r)
	})
}
func (s *Server) parseForm(w http.ResponseWriter, r *http.Request) error {
	limit := int64(64 << 10)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		limit = (s.cfg.Storage.MaxUploadMB << 20) + (1 << 20)
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(2 << 20)
	}
	return r.ParseForm()
}
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Username"] = s.cfg.Admin.Username
	if sess, ok := s.auth.current(r); ok {
		data["CSRF"] = sess.CSRF
	}
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, name, data); err != nil {
		slog.Error("render template", "error", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}
func (s *Server) problem(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, "error.html", map[string]any{"Title": "操作未完成", "Error": message})
}
func (s *Server) dbError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		s.problem(w, r, 404, "内容不存在")
		return
	}
	if errors.Is(err, database.ErrConflict) {
		s.problem(w, r, 409, "包名已存在，或应用仍有版本记录")
		return
	}
	slog.Error("database operation", "route", r.Pattern, "error", err)
	s.problem(w, r, 500, "操作失败，请稍后重试")
}
func idOf(r *http.Request) int64 { id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return id }
func redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusSeeOther)
}
func formatSize(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
func formatDate(s string) string {
	if s == "" {
		return "—"
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}
