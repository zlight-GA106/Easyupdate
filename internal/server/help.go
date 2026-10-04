package server

import "net/http"

func (s *Server) help(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "help.html", map[string]any{
		"Title":       "说明",
		"Nav":         "help",
		"PublicURL":   s.cfg.Server.PublicURL,
		"MaxUploadMB": s.cfg.Storage.MaxUploadMB,
	})
}
