package server

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"github.com/zlight-GA106/EasyUpdate/internal/github"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func (s *Server) saveGitHub(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.App(r.Context(), idOf(r)); err != nil {
		s.dbError(w, r, err)
		return
	}
	source := database.GitHubSource{AppID: idOf(r), Owner: strings.TrimSpace(r.FormValue("owner")), Repo: strings.TrimSpace(r.FormValue("repo")), AssetPattern: strings.TrimSpace(r.FormValue("asset_pattern")), Enabled: r.FormValue("enabled") == "on"}
	_, err := path.Match(source.AssetPattern, "app.apk")
	if !ownerPattern.MatchString(source.Owner) || !repoPattern.MatchString(source.Repo) || source.Repo == "." || source.Repo == ".." || source.AssetPattern == "" || len(source.AssetPattern) > 128 || strings.ContainsAny(source.AssetPattern, "/\\") || err != nil {
		s.problem(w, r, 400, "请检查 GitHub 仓库和文件匹配规则")
		return
	}
	if err = s.db.SaveGitHubSource(r.Context(), source); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, fmt.Sprintf("/admin/apps/%d", source.AppID))
}
func (s *Server) syncGitHub(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.App(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	source, err := s.db.GitHubSource(r.Context(), a.ID)
	if err != nil || !source.Enabled {
		s.problem(w, r, 400, "请先启用 GitHub 来源")
		return
	}
	client := github.New(s.cfg.GitHub.Token)
	items, err := client.Releases(r.Context(), source.Owner, source.Repo)
	s.db.GitHubChecked(r.Context(), a.ID, 0)
	if err != nil {
		s.githubError(w, r, err)
		return
	}
	for _, item := range items {
		if item.Draft {
			continue
		}
		imported, err := s.db.GitHubImported(r.Context(), a.ID, item.ID)
		if err != nil {
			s.dbError(w, r, err)
			return
		}
		if imported {
			continue
		}
		matches := []github.Asset{}
		for _, asset := range item.Assets {
			match, _ := path.Match(source.AssetPattern, asset.Name)
			if match && strings.EqualFold(path.Ext(asset.Name), ".apk") {
				matches = append(matches, asset)
			}
		}
		if len(matches) == 0 {
			continue
		}
		if len(matches) > 1 {
			s.problem(w, r, 409, "匹配到多个 APK，请调整匹配规则")
			return
		}
		asset := matches[0]
		if asset.Size > s.storage.MaxBytes {
			s.problem(w, r, 413, "GitHub APK 超出上传上限")
			return
		}
		stream, err := client.Download(r.Context(), asset.DownloadURL)
		if err != nil {
			s.githubError(w, r, err)
			return
		}
		file, err := s.storage.Stage(stream)
		stream.Close()
		if err != nil {
			s.uploadError(w, r, err)
			return
		}
		if asset.Size > 0 && asset.Size != file.Size {
			s.storage.Discard(file)
			s.problem(w, r, 502, "GitHub APK 下载不完整")
			return
		}
		if file.Metadata.PackageName != "" && file.Metadata.PackageName != a.PackageName {
			s.storage.Discard(file)
			s.problem(w, r, 400, "GitHub APK 包名与应用不一致")
			return
		}
		if file.Metadata.VersionCode > 0 {
			if _, err = s.db.ReleaseByCode(r.Context(), a.ID, file.Metadata.VersionCode); err == nil {
				s.storage.Discard(file)
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				s.storage.Discard(file)
				s.dbError(w, r, err)
				return
			}
		}
		notes := item.Body
		if len(notes) > 16384 {
			notes = strings.ToValidUTF8(notes[:16000], "")
		}
		s.showConfirmation(w, r, a, pendingUpload{File: file, AppID: a.ID, Filename: asset.Name, Notes: notes, Tag: item.Tag, GitHubID: item.ID, Expires: time.Now().Add(30 * time.Minute)})
		slog.Info("GitHub sync", "app_id", a.ID, "release_id", item.ID)
		return
	}
	s.problem(w, r, 409, "最近 100 个 Release 中没有可导入的 APK")
}
func (s *Server) githubError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("GitHub sync error", "error", err)
	s.problem(w, r, 502, "GitHub 同步失败："+err.Error())
}
