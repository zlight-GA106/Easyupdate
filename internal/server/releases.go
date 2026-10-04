package server

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"github.com/zlight-GA106/EasyUpdate/internal/storage"
)

type pendingUpload struct {
	File                        storage.Staged
	AppID                       int64
	Owner, Filename, Notes, Tag string
	GitHubID                    int64
	Expires                     time.Time
}

func (s *Server) uploadPage(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "upload.html", map[string]any{"Title": "上传 APK", "Nav": "apps", "App": a})
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	file, header, err := r.FormFile("apk")
	if err != nil {
		s.problem(w, r, 400, "请选择 APK")
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".apk") {
		s.problem(w, r, 400, "文件扩展名必须为 .apk")
		return
	}
	staged, err := s.storage.Stage(file)
	if err != nil {
		s.uploadError(w, r, err)
		return
	}
	p := pendingUpload{File: staged, AppID: a.ID, Filename: filepath.Base(header.Filename), Expires: time.Now().Add(30 * time.Minute)}
	s.showConfirmation(w, r, a, p)
}
func (s *Server) uploadError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("APK upload error", "error", err)
	if errors.Is(err, storage.ErrTooLarge) {
		s.problem(w, r, 413, "APK 超出上传上限")
		return
	}
	s.problem(w, r, 400, "无法读取 APK，请检查文件格式")
}
func (s *Server) showConfirmation(w http.ResponseWriter, r *http.Request, a database.App, p pendingUpload) {
	if p.File.Metadata.PackageName != "" && p.File.Metadata.PackageName != a.PackageName {
		s.storage.Discard(p.File)
		s.problem(w, r, 400, "APK 包名与应用不一致")
		return
	}
	sess, _ := s.auth.current(r)
	p.Owner = sess.CSRF
	token := randomToken()
	s.uploadMu.Lock()
	for key, old := range s.pending {
		if time.Now().After(old.Expires) {
			s.storage.Discard(old.File)
			delete(s.pending, key)
		}
	}
	if len(s.pending) >= 32 {
		s.uploadMu.Unlock()
		s.storage.Discard(p.File)
		s.problem(w, r, 429, "待确认文件过多，请稍后重试")
		return
	}
	s.pending[token] = p
	s.uploadMu.Unlock()
	s.render(w, r, "upload-confirm.html", map[string]any{"Title": "确认版本", "Nav": "apps", "App": a, "Upload": p, "Token": token, "Metadata": p.File.Metadata})
	slog.Info("APK uploaded", "app_id", a.ID, "size", p.File.Size, "metadata", p.File.Metadata.Source)
}
func (s *Server) createRelease(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	sess, _ := s.auth.current(r)
	token := r.FormValue("upload_token")
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	p, ok := s.pending[token]
	if !ok || p.AppID != a.ID || p.Owner != sess.CSRF || time.Now().After(p.Expires) {
		s.problem(w, r, 400, "上传已过期，请重新选择 APK")
		return
	}
	if r.FormValue("action") == "cancel" {
		s.storage.Discard(p.File)
		delete(s.pending, token)
		redirect(w, r, fmt.Sprintf("/admin/apps/%d", a.ID))
		return
	}
	code, err := strconv.ParseInt(r.FormValue("version_code"), 10, 64)
	name := strings.TrimSpace(r.FormValue("version_name"))
	pkg := strings.TrimSpace(r.FormValue("package_name"))
	notes := strings.TrimSpace(r.FormValue("release_notes"))
	if err != nil || code <= 0 || code > 2147483647 || name == "" || len(name) > 128 || len(notes) > 16384 {
		s.problem(w, r, 400, "请检查版本名称和版本号")
		return
	}
	if pkg != a.PackageName || p.File.Metadata.PackageName != "" && p.File.Metadata.PackageName != pkg {
		s.problem(w, r, 400, "APK 包名与应用不一致")
		return
	}
	if p.File.Metadata.VersionCode > 0 && p.File.Metadata.VersionCode != code || p.File.Metadata.VersionName != "" && p.File.Metadata.VersionName != name {
		s.problem(w, r, 400, "版本信息必须与 APK 一致")
		return
	}
	if _, e := s.db.ReleaseByCode(r.Context(), a.ID, code); e == nil {
		s.problem(w, r, 409, "该版本号已存在")
		return
	} else if !errors.Is(e, sql.ErrNoRows) {
		s.dbError(w, r, e)
		return
	}
	path, err := s.storage.Commit(p.File, a.ID, code)
	if err != nil {
		slog.Error("APK commit", "error", err)
		s.problem(w, r, 409, "版本文件已存在或无法保存")
		return
	}
	rel := database.Release{AppID: a.ID, VersionName: name, VersionCode: code, ReleaseNotes: notes, APKFilename: p.Filename, APKPath: path, APKSize: p.File.Size, APKSHA256: p.File.SHA256, Mandatory: r.FormValue("mandatory") == "on", GitHubReleaseID: p.GitHubID, GitHubTag: p.Tag}
	id, err := s.db.CreateRelease(r.Context(), rel)
	if err != nil {
		s.storage.Delete(a.ID, code)
		delete(s.pending, token)
		s.dbError(w, r, err)
		return
	}
	delete(s.pending, token)
	slog.Info("release created", "release_id", id, "app_id", a.ID, "version_code", code)
	redirect(w, r, fmt.Sprintf("/admin/releases/%d", id))
}
func (s *Server) releasePage(w http.ResponseWriter, r *http.Request) {
	rel, err := s.db.Release(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "release.html", map[string]any{"Title": rel.AppName + " " + rel.VersionName, "Nav": "apps", "Release": rel, "DownloadURL": s.cfg.Server.PublicURL + downloadRoute(rel.PackageName, rel.VersionCode)})
}
func (s *Server) editRelease(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Release(r.Context(), idOf(r)); err != nil {
		s.dbError(w, r, err)
		return
	}
	notes := strings.TrimSpace(r.FormValue("release_notes"))
	if len(notes) > 16384 {
		s.problem(w, r, 400, "版本说明过长")
		return
	}
	if err := s.db.EditRelease(r.Context(), idOf(r), notes, r.FormValue("mandatory") == "on"); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, fmt.Sprintf("/admin/releases/%d", idOf(r)))
}
func (s *Server) publishRelease(w http.ResponseWriter, r *http.Request) {
	rel, err := s.db.Release(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	published := r.FormValue("action") == "publish"
	if err = s.db.Publish(r.Context(), rel.ID, published); err != nil {
		s.dbError(w, r, err)
		return
	}
	slog.Info("release published", "release_id", rel.ID, "published", published)
	redirect(w, r, fmt.Sprintf("/admin/releases/%d", rel.ID))
}
func (s *Server) deleteReleasePage(w http.ResponseWriter, r *http.Request) {
	rel, err := s.db.Release(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "delete.html", map[string]any{"Title": "删除版本", "Nav": "apps", "ConfirmName": strconv.FormatInt(rel.VersionCode, 10), "Back": fmt.Sprintf("/admin/releases/%d", rel.ID)})
}
func (s *Server) deleteRelease(w http.ResponseWriter, r *http.Request) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	rel, err := s.db.Release(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	if r.FormValue("confirmation") != strconv.FormatInt(rel.VersionCode, 10) {
		s.problem(w, r, 400, "请输入版本号确认删除")
		return
	}
	// Remove the row first: public APIs stop offering the APK immediately.
	if err = s.db.DeleteRelease(r.Context(), rel.ID); err != nil {
		s.dbError(w, r, err)
		return
	}
	if err = s.storage.Delete(rel.AppID, rel.VersionCode); err != nil {
		slog.Error("APK deletion", "release_id", rel.ID, "error", err)
		s.problem(w, r, 500, "版本记录已删除，但文件清理失败；请检查存储目录")
		return
	}
	redirect(w, r, fmt.Sprintf("/admin/apps/%d", rel.AppID))
}
func downloadRoute(pkg string, code int64) string {
	return fmt.Sprintf("/api/v1/apps/%s/releases/%d/download", pkg, code)
}
