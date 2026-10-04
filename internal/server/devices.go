package server

import (
	"encoding/json"
	"errors"
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
	if id > 0 {
		if _, err := s.db.App(r.Context(), id); err != nil {
			s.dbError(w, r, err)
			return
		}
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

func (s *Server) deviceForm(w http.ResponseWriter, r *http.Request) {
	d := database.Device{}
	title := "新建设备"
	if idOf(r) > 0 {
		var err error
		d, err = s.db.Device(r.Context(), idOf(r))
		if err != nil {
			s.dbError(w, r, err)
			return
		}
		title = "编辑设备"
	} else if value := r.URL.Query().Get("app_id"); value != "" {
		appID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || appID < 0 {
			s.problem(w, r, 400, "应用编号无效")
			return
		}
		if appID > 0 {
			if _, err = s.db.App(r.Context(), appID); err != nil {
				s.dbError(w, r, err)
				return
			}
		}
		d.AppID = appID
	}
	apps, err := s.db.Apps(r.Context())
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "device-form.html", map[string]any{"Title": title, "Nav": "devices", "Device": d, "Apps": apps})
}

func (s *Server) saveDevice(w http.ResponseWriter, r *http.Request) {
	id := idOf(r)
	if id > 0 {
		if _, err := s.db.Device(r.Context(), id); err != nil {
			s.dbError(w, r, err)
			return
		}
	}
	appID, appErr := strconv.ParseInt(r.FormValue("app_id"), 10, 64)
	versionCode, versionErr := strconv.ParseInt(r.FormValue("version_code"), 10, 64)
	d := database.Device{
		ID:          id,
		DeviceID:    strings.ToLower(strings.TrimSpace(r.FormValue("device_id"))),
		AppID:       appID,
		VersionName: strings.TrimSpace(r.FormValue("version_name")),
		VersionCode: versionCode,
	}
	if !uuidPattern.MatchString(d.DeviceID) {
		s.problem(w, r, 400, "请输入有效的设备 UUID")
		return
	}
	if appErr != nil || d.AppID <= 0 || versionErr != nil || d.VersionCode < 0 || d.VersionCode > 2147483647 || d.VersionName == "" || len(d.VersionName) > 128 {
		s.problem(w, r, 400, "请检查应用和版本信息")
		return
	}
	if _, err := s.db.SaveDevice(r.Context(), d); err != nil {
		if errors.Is(err, database.ErrConflict) {
			s.problem(w, r, 409, "该应用已有此设备记录")
			return
		}
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/devices")
}

func (s *Server) deleteDevicePage(w http.ResponseWriter, r *http.Request) {
	d, err := s.db.Device(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "delete.html", map[string]any{
		"Title":        "删除设备",
		"Nav":          "devices",
		"ConfirmName":  d.DeviceID,
		"Back":         "/admin/devices",
		"DeleteNotice": "删除后，客户端再次发送心跳会重新记录。",
	})
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.problem(w, r, 405, "请提交确认表单删除")
		return
	}
	d, err := s.db.Device(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	confirmation := strings.ToLower(strings.TrimSpace(r.FormValue("confirmation")))
	if confirmation != d.DeviceID {
		s.problem(w, r, 400, "请输入完整设备 UUID 确认删除")
		return
	}
	if err = s.db.DeleteDevice(r.Context(), d.ID, confirmation); err != nil {
		if errors.Is(err, database.ErrConflict) {
			s.problem(w, r, 409, "设备信息已修改，请刷新后重试")
			return
		}
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/devices")
}
