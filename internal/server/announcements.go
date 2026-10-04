package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

func (s *Server) publicAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Announcements(r.Context(), true)
	if err != nil {
		s.apiDBError(w, err, "not_found")
		return
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}
func (s *Server) announcements(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Announcements(r.Context(), false)
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "announcements.html", map[string]any{"Title": "公告", "Nav": "announcements", "Items": items})
}
func (s *Server) announcementForm(w http.ResponseWriter, r *http.Request) {
	a := database.Announcement{}
	if idOf(r) > 0 {
		var err error
		a, err = s.db.Announcement(r.Context(), idOf(r))
		if err != nil {
			s.dbError(w, r, err)
			return
		}
	}
	title := "新建公告"
	if a.ID > 0 {
		title = "编辑公告"
	}
	s.render(w, r, "announcement-form.html", map[string]any{"Title": title, "Nav": "announcements", "Item": a})
}
func (s *Server) saveAnnouncement(w http.ResponseWriter, r *http.Request) {
	id := idOf(r)
	if id > 0 {
		if _, err := s.db.Announcement(r.Context(), id); err != nil {
			s.dbError(w, r, err)
			return
		}
	}
	a := database.Announcement{ID: id, Title: strings.TrimSpace(r.FormValue("title")), Content: strings.TrimSpace(r.FormValue("content")), Published: r.FormValue("published") == "on"}
	if a.Title == "" || len(a.Title) > 200 || a.Content == "" || len(a.Content) > 16384 {
		s.problem(w, r, 400, "请检查公告标题和内容")
		return
	}
	if _, err := s.db.SaveAnnouncement(r.Context(), a); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/announcements")
}
func (s *Server) publishAnnouncement(w http.ResponseWriter, r *http.Request) {
	if action := r.FormValue("action"); action != "publish" && action != "unpublish" {
		s.problem(w, r, 400, "发布操作无效")
		return
	}
	if _, err := s.db.Announcement(r.Context(), idOf(r)); err != nil {
		s.dbError(w, r, err)
		return
	}
	if err := s.db.PublishAnnouncement(r.Context(), idOf(r), r.FormValue("action") == "publish"); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/announcements")
}
func (s *Server) deleteAnnouncementPage(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.Announcement(r.Context(), idOf(r))
	if err != nil {
		s.dbError(w, r, err)
		return
	}
	s.render(w, r, "delete.html", map[string]any{"Title": "删除公告", "Nav": "announcements", "ConfirmName": strconv.FormatInt(a.ID, 10), "Back": "/admin/announcements"})
}
func (s *Server) deleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("confirmation") != fmt.Sprint(idOf(r)) {
		s.problem(w, r, 400, "请输入公告编号确认删除")
		return
	}
	if err := s.db.DeleteAnnouncement(r.Context(), idOf(r)); err != nil {
		s.dbError(w, r, err)
		return
	}
	redirect(w, r, "/admin/announcements")
}
