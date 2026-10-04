package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func TestHeartbeatRejectsInvalidInputs(t *testing.T) {
	s := testServer(t)
	for _, body := range []string{`{"device_id":"wrong"}`, strings.Repeat("a", 5000), `{} {}`, `{"device_id":"a94187b3-cc2f-4ef0-93fb-04e7e3444342","package_name":"com.example.app","version_name":"v1","version_code":-1}`} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/heartbeat", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("validation: %d", w.Code)
		}
	}
}

func deviceTestSession(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	w := httptest.NewRecorder()
	sess := s.auth.create(w, httptest.NewRequest(http.MethodGet, "/", nil), true)
	return w.Result().Cookies()[0], sess.CSRF
}

func deviceTestRequest(s *Server, cookie *http.Cookie, method, path string, values url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	if values != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func deviceTestApp(t *testing.T, s *Server, name, packageName string) int64 {
	t.Helper()
	id, err := s.db.SaveApp(context.Background(), database.App{Name: name, PackageName: packageName})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func deviceTestFields(csrf string, appID int64, uuid string) url.Values {
	return url.Values{
		"csrf":         {csrf},
		"device_id":    {uuid},
		"app_id":       {fmt.Sprint(appID)},
		"version_name": {"1.0"},
		"version_code": {"1"},
	}
}

func TestDeviceAdminCRUDPreservesTimesAndHeartbeat(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	appID := deviceTestApp(t, s, "App", "com.example.app")
	otherAppID := deviceTestApp(t, s, "Other", "com.example.other")
	cookie, csrf := deviceTestSession(t, s)
	uuid := "a94187b3-cc2f-4ef0-93fb-04e7e3444342"
	form := deviceTestFields(csrf, appID, strings.ToUpper(uuid))
	form.Set("first_seen", "untrusted")
	form.Set("last_seen", "untrusted")
	if w := deviceTestRequest(s, cookie, "GET", fmt.Sprintf("/admin/devices/new?app_id=%d", appID), nil); w.Code != 200 || !strings.Contains(w.Body.String(), "新建设备") {
		t.Fatalf("new form: %d %s", w.Code, w.Body.String())
	}
	w := deviceTestRequest(s, cookie, "POST", "/admin/devices/new", form)
	if w.Code != 303 || w.Header().Get("Location") != "/admin/devices" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	items, err := s.db.Devices(ctx, appID, 10, 0)
	if err != nil || len(items) != 1 || items[0].DeviceID != uuid || items[0].FirstSeen == "untrusted" || items[0].LastSeen != "" {
		t.Fatalf("created device: %+v err=%v", items, err)
	}
	original := items[0]
	otherID, err := s.db.SaveDevice(ctx, database.Device{DeviceID: uuid, AppID: otherAppID, VersionName: "Other", VersionCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.db.Device(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	w = deviceTestRequest(s, cookie, "GET", "/admin/devices", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), fmt.Sprintf(`href="/admin/devices/%d/edit"`, original.ID)) || !strings.Contains(w.Body.String(), `class="row-actions"`) || !strings.Contains(w.Body.String(), "最近上报") {
		t.Fatalf("list controls: %d %s", w.Code, w.Body.String())
	}
	editPath := fmt.Sprintf("/admin/devices/%d/edit", original.ID)
	if w := deviceTestRequest(s, cookie, "GET", editPath, nil); w.Code != 200 || !strings.Contains(w.Body.String(), uuid) {
		t.Fatalf("edit form: %d %s", w.Code, w.Body.String())
	}
	newUUID := "b94187b3-cc2f-4ef0-93fb-04e7e3444342"
	form.Set("device_id", strings.ToUpper(newUUID))
	form.Set("app_id", fmt.Sprint(otherAppID))
	form.Set("version_name", "2.0")
	form.Set("version_code", "2")
	form.Set("id", fmt.Sprint(otherID))
	w = deviceTestRequest(s, cookie, "POST", editPath, form)
	if w.Code != 303 {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	edited, err := s.db.Device(ctx, original.ID)
	if err != nil || edited.DeviceID != newUUID || edited.AppID != otherAppID || edited.VersionCode != 2 || edited.VersionName != "2.0" || edited.FirstSeen != original.FirstSeen || edited.LastSeen != original.LastSeen {
		t.Fatalf("edited device: %+v err=%v", edited, err)
	}
	if got, err := s.db.Device(ctx, otherID); err != nil || got != other {
		t.Fatalf("edit modified another ID: %+v err=%v", got, err)
	}
	body := fmt.Sprintf(`{"device_id":%q,"package_name":"com.example.other","version_name":"3.0","version_code":3}`, strings.ToUpper(newUUID))
	r := httptest.NewRequest("POST", "/api/v1/heartbeat", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	afterHeartbeat, err := s.db.Device(ctx, original.ID)
	if err != nil || afterHeartbeat.VersionCode != 3 || afterHeartbeat.FirstSeen != original.FirstSeen || afterHeartbeat.LastSeen == "" {
		t.Fatalf("heartbeat after manual edit: %+v err=%v", afterHeartbeat, err)
	}
	if got, err := s.db.Device(ctx, otherID); err != nil || got != other {
		t.Fatalf("heartbeat modified another record: %+v err=%v", got, err)
	}
}

func TestDeviceAdminRejectsInvalidWritesAndDuplicateKeys(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	appID := deviceTestApp(t, s, "App", "com.example.app")
	cookie, csrf := deviceTestSession(t, s)
	uuid := "a94187b3-cc2f-4ef0-93fb-04e7e3444342"
	for _, tc := range []struct {
		name, field, value string
		status             int
	}{
		{"bad UUID", "device_id", "not-a-uuid", 400},
		{"bad UUID variant", "device_id", "a94187b3-cc2f-4ef0-03fb-04e7e3444342", 400},
		{"zero app", "app_id", "0", 400},
		{"invalid app", "app_id", "wrong", 400},
		{"missing app", "app_id", "999", 404},
		{"empty version", "version_name", "   ", 400},
		{"long version", "version_name", strings.Repeat("v", 129), 400},
		{"negative code", "version_code", "-1", 400},
		{"large code", "version_code", "2147483648", 400},
		{"invalid code", "version_code", "1.5", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := deviceTestFields(csrf, appID, uuid)
			form.Set(tc.field, tc.value)
			if w := deviceTestRequest(s, cookie, "POST", "/admin/devices/new", form); w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			items, err := s.db.Devices(ctx, 0, 10, 0)
			if err != nil || len(items) != 0 {
				t.Fatalf("invalid create wrote data: %+v err=%v", items, err)
			}
		})
	}
	firstID, err := s.db.SaveDevice(ctx, database.Device{DeviceID: uuid, AppID: appID, VersionName: "1", VersionCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := s.db.SaveDevice(ctx, database.Device{DeviceID: "b94187b3-cc2f-4ef0-93fb-04e7e3444342", AppID: appID, VersionName: "2", VersionCode: 2})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.db.Device(ctx, secondID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/devices/new", fmt.Sprintf("/admin/devices/%d/edit", secondID)} {
		w := deviceTestRequest(s, cookie, "POST", path, deviceTestFields(csrf, appID, strings.ToUpper(uuid)))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "该应用已有此设备记录") {
			t.Fatalf("duplicate: %d %s", w.Code, w.Body.String())
		}
	}
	if got, err := s.db.Device(ctx, secondID); err != nil || got != before {
		t.Fatalf("duplicate edit modified target: %+v err=%v", got, err)
	}
	missingAppForm := deviceTestFields(csrf, 999, uuid)
	if w := deviceTestRequest(s, cookie, "POST", fmt.Sprintf("/admin/devices/%d/edit", firstID), missingAppForm); w.Code != 404 {
		t.Fatalf("missing application edit: %d", w.Code)
	}
	for _, path := range []string{"/admin/devices/999/edit", "/admin/devices/999/delete"} {
		for _, method := range []string{"GET", "POST"} {
			if w := deviceTestRequest(s, cookie, method, path, deviceTestFields(csrf, appID, uuid)); w.Code != 404 {
				t.Fatalf("missing device %s %s: %d", method, path, w.Code)
			}
		}
	}
	items, err := s.db.Devices(ctx, 0, 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("conflict or missing identity changed count: %+v err=%v", items, err)
	}
}

func TestDeviceDeleteRequiresLoginCSRFAndCorrectConfirmation(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	appID := deviceTestApp(t, s, "App", "com.example.app")
	cookie, csrf := deviceTestSession(t, s)
	uuid := "a94187b3-cc2f-4ef0-93fb-04e7e3444342"
	id, err := s.db.SaveDevice(ctx, database.Device{DeviceID: uuid, AppID: appID, VersionName: "1", VersionCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := s.db.SaveDevice(ctx, database.Device{DeviceID: "b94187b3-cc2f-4ef0-93fb-04e7e3444342", AppID: appID, VersionName: "2", VersionCode: 2})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.db.Device(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	deletePath := fmt.Sprintf("/admin/devices/%d/delete", id)
	paths := []string{"/admin/devices", "/admin/devices/new", fmt.Sprintf("/admin/devices/%d/edit", id), deletePath}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST"} {
			if path == "/admin/devices" && method == "POST" {
				continue
			}
			w := deviceTestRequest(s, nil, method, path, url.Values{"confirmation": {uuid}, "csrf": {csrf}})
			if w.Code != 303 || w.Header().Get("Location") != "/login" {
				t.Fatalf("anonymous %s %s: %d", method, path, w.Code)
			}
		}
	}
	for _, path := range []string{"/admin/devices/new", fmt.Sprintf("/admin/devices/%d/edit", id), deletePath} {
		w := deviceTestRequest(s, cookie, "POST", path, url.Values{"confirmation": {uuid}})
		if w.Code != 403 {
			t.Fatalf("missing CSRF %s: %d", path, w.Code)
		}
	}
	w := deviceTestRequest(s, cookie, "GET", deletePath+"?confirmation="+uuid, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "客户端再次发送心跳会重新记录") {
		t.Fatalf("confirmation page: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.db.Device(ctx, id); err != nil {
		t.Fatalf("GET deleted record: %v", err)
	}
	w = deviceTestRequest(s, cookie, "POST", deletePath, url.Values{"csrf": {csrf}, "confirmation": {"wrong"}})
	if w.Code != 400 {
		t.Fatalf("wrong confirmation: %d", w.Code)
	}
	if _, err := s.db.Device(ctx, id); err != nil {
		t.Fatalf("wrong confirmation deleted record: %v", err)
	}
	if w := deviceTestRequest(s, cookie, "DELETE", deletePath, nil); w.Code != 405 {
		t.Fatalf("unsupported deletion method: %d", w.Code)
	}
	w = deviceTestRequest(s, cookie, "POST", deletePath, url.Values{"csrf": {csrf}, "confirmation": {uuid}})
	if w.Code != 303 || w.Header().Get("Location") != "/admin/devices" {
		t.Fatalf("confirmed delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.db.Device(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("record still exists: %v", err)
	}
	if got, err := s.db.Device(ctx, otherID); err != nil || got != other {
		t.Fatalf("delete affected another record: %+v err=%v", got, err)
	}
	body := fmt.Sprintf(`{"device_id":%q,"package_name":"com.example.app","version_name":"3","version_code":3}`, uuid)
	r := httptest.NewRequest("POST", "/api/v1/heartbeat", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("heartbeat after deletion: %d", w.Code)
	}
	items, err := s.db.Devices(ctx, appID, 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("heartbeat did not recreate deleted record: %+v err=%v", items, err)
	}
	var recreated bool
	for _, item := range items {
		if item.DeviceID == uuid && item.VersionCode == 3 && item.LastSeen != "" {
			recreated = true
		}
	}
	if !recreated {
		t.Fatalf("missing recreated device: %+v", items)
	}
}
