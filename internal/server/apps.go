package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

var packagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func validPackage(p string) bool { return len(p) <= 255 && packagePattern.MatchString(p) }
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	var apps, releases, devices int
	err := s.db.DB.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM apps),(SELECT COUNT(*) FROM releases WHERE published=1),(SELECT COUNT(*) FROM devices)`).Scan(&apps, &releases, &devices)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	recent, err := s.db.Releases(r.Context(), 0)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	heartbeats, err := s.db.Devices(r.Context(), 0, 5, 0)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "overview.html", map[string]any{"Title": "概览", "Nav": "overview", "AppCount": apps, "ReleaseCount": releases, "DeviceCount": devices, "Releases": recent, "Devices": heartbeats})
}
func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Apps(r.Context())
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "apps.html", map[string]any{"Title": "应用", "Nav": "apps", "Apps": items})
}
func (s *Server) appForm(w http.ResponseWriter, r *http.Request) {
	a := database.App{}
	if idOf(r) > 0 {
		var err error
		a, err = s.db.App(r.Context(), idOf(r))
		if err != nil {
			s.dbError(w, r, err)
			return
		}
	}
	title := "新建应用"
	if a.ID > 0 {
		title = "编辑应用"
	}
	s.render(w, r, "app-form.html", map[string]any{"Title": title, "Nav": "apps", "App": a})
}
func (s *Server) saveApp(w http.ResponseWriter, r *http.Request) {
	a := database.App{ID: idOf(r), Name: strings.TrimSpace(r.FormValue("name")), PackageName: strings.TrimSpace(r.FormValue("package_name")), Description: strings.TrimSpace(r.FormValue("description"))}
	if a.Name == "" || len(a.Name) > 128 || !validPackage(a.PackageName) || len(a.Description) > 4096 {
		s.problem(w, r, 400, "请检查应用名称和包名")
		return
	}
	id, err := s.db.SaveApp(r.Context(), a)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	slogApp(a, id)
	redirect(w, r, fmt.Sprintf("/admin/apps/%d", id))
}
func (s *Server) appDetail(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	releases, err := s.db.Releases(r.Context(), a.ID)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	latest, _ := s.db.Latest(r.Context(), a.ID)
	source, _ := s.db.GitHubSource(r.Context(), a.ID)
	s.render(w, r, "app.html", map[string]any{"Title": a.Name, "Nav": "apps", "App": a, "Releases": releases, "Latest": latest, "Source": source, "GitHubEnabled": true})
}
func (s *Server) deleteAppPage(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "delete.html", map[string]any{"Title": "删除应用", "Nav": "apps", "ConfirmName": a.PackageName, "Back": fmt.Sprintf("/admin/apps/%d", a.ID)})
}
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	if r.FormValue("confirmation") != a.PackageName {
		s.problem(w, r, 400, "请输入完整包名确认删除")
		return
	}
	if err = s.db.DeleteApp(r.Context(), a.ID); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/apps")
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "settings.html", map[string]any{"Title": "设置", "Nav": "settings", "PublicURL": s.cfg.Server.PublicURL, "MaxUploadMB": s.cfg.Storage.MaxUploadMB, "Listen": s.cfg.Server.Listen, "DatabasePath": s.cfg.Database.Path, "StoragePath": s.cfg.Storage.Path, "GitHubTokenConfigured": s.cfg.GitHub.Token != ""})
}
