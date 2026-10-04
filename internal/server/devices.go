package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var v struct {
		DeviceID    string `json:"device_id"`
		PackageName string `json:"package_name"`
		VersionName string `json:"version_name"`
		VersionCode int64  `json:"version_code"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		apiError(w, 400, "invalid_body", "Expected a JSON object up to 4 KB")
		return
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		apiError(w, 400, "invalid_body", "Expected one JSON object")
		return
	}
	if !uuidPattern.MatchString(v.DeviceID) {
		slog.Warn("heartbeat validation error", "field", "device_id")
		apiError(w, 400, "invalid_device_id", "Expected a UUID")
		return
	}
	if !validPackage(v.PackageName) || v.VersionName == "" || len(v.VersionName) > 128 || v.VersionCode < 0 || v.VersionCode > 2147483647 {
		apiError(w, 400, "invalid_version", "Invalid package or version")
		return
	}
	a, err := s.db.AppByPackage(r.Context(), v.PackageName)
	if err != nil {
		s.apiDBError(w, err, "app_not_found")
		return
	}
	if err = s.db.Heartbeat(r.Context(), database.Device{DeviceID: strings.ToLower(v.DeviceID), AppID: a.ID, VersionName: v.VersionName, VersionCode: v.VersionCode}); err != nil {
		s.apiDBError(w, err, "app_not_found")
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("app_id"), 10, 64)
	if r.URL.Query().Get("app_id") != "" && (err != nil || id < 0) {
		s.problem(w, r, 400, "应用筛选无效")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		s.problem(w, r, 400, "页码无效")
		return
	}
	items, err := s.db.Devices(r.Context(), id, 51, (page-1)*50)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	hasNext := len(items) > 50
	if hasNext {
		items = items[:50]
	}
	apps, err := s.db.Apps(r.Context())
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "devices.html", map[string]any{"Title": "设备", "Nav": "devices", "Devices": items, "Apps": apps, "AppID": id, "Page": page, "Previous": page - 1, "Next": page + 1, "HasNext": hasNext})
}
