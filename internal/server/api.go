package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, map[string]string{"error": code, "message": message})
}
func (s *Server) apiDBError(w http.ResponseWriter, err error, missing string) {
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, 404, missing, "Not found")
		return
	}
	slog.Error("API database error", "error", err)
	apiError(w, 500, "internal_error", "Request failed")
}
func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	var current int64
	if raw, ok := r.URL.Query()["version_code"]; ok {
		if len(raw) != 1 {
			apiError(w, 400, "invalid_version", "Invalid version_code")
			return
		}
		var err error
		current, err = strconv.ParseInt(raw[0], 10, 64)
		if err != nil || current < 0 || current > 2147483647 {
			apiError(w, 400, "invalid_version", "Invalid version_code")
			return
		}
	}
	app, err := s.db.AppByPackage(r.Context(), r.PathValue("packageName"))
	if err != nil {
		s.apiDBError(w, err, "app_not_found")
		return
	}
	rel, err := s.db.Latest(r.Context(), app.ID)
	if err != nil {
		s.apiDBError(w, err, "no_release")
		return
	}
	if current >= rel.VersionCode {
		jsonResponse(w, 200, map[string]any{"package_name": app.PackageName, "update_available": false, "latest_version_name": rel.VersionName, "latest_version_code": rel.VersionCode})
		return
	}
	jsonResponse(w, 200, map[string]any{"package_name": app.PackageName, "update_available": true, "version_name": rel.VersionName, "version_code": rel.VersionCode, "mandatory": rel.Mandatory, "published_at": rel.PublishedAt, "release_notes": rel.ReleaseNotes, "download_url": s.cfg.Server.PublicURL + downloadRoute(app.PackageName, rel.VersionCode), "size": rel.APKSize, "sha256": rel.APKSHA256})
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.ParseInt(r.PathValue("versionCode"), 10, 64)
	if err != nil || code <= 0 {
		apiError(w, 400, "invalid_version", "Invalid version_code")
		return
	}
	app, err := s.db.AppByPackage(r.Context(), r.PathValue("packageName"))
	if err != nil {
		s.apiDBError(w, err, "app_not_found")
		return
	}
	rel, err := s.db.ReleaseByCode(r.Context(), app.ID, code)
	if err != nil {
		s.apiDBError(w, err, "release_not_found")
		return
	}
	if !rel.Published {
		apiError(w, 404, "release_not_found", "Not found")
		return
	}
	// Only internally computed IDs determine the disk path.
	f, err := os.Open(s.storage.Path(app.ID, code))
	if err != nil {
		slog.Error("download error", "release_id", rel.ID, "error", err)
		apiError(w, 500, "download_error", "APK unavailable")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != rel.APKSize {
		slog.Error("download size mismatch", "release_id", rel.ID)
		apiError(w, 500, "download_error", "APK unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%d.apk"`, app.PackageName, code))
	w.Header().Set("ETag", `"`+rel.APKSHA256+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "app.apk", info.ModTime(), f)
}
