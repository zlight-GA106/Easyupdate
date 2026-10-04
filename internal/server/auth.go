package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "easyupdate_session"

type session struct {
	CSRF     string
	LoggedIn bool
	Expires  time.Time
}
type attempts struct {
	Count int
	Until time.Time
}
type auth struct {
	mu       sync.Mutex
	username string
	hash     []byte
	secure   bool
	sessions map[string]session
	failures map[string]attempts
}

func newAuth(username, password string, secure bool) (*auth, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &auth{username: username, hash: h, secure: secure, sessions: map[string]session{}, failures: map[string]attempts{}}, nil
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (a *auth) current(r *http.Request) (session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if ok && time.Now().After(s.Expires) {
		delete(a.sessions, c.Value)
		ok = false
	}
	return s, ok
}
func (a *auth) create(w http.ResponseWriter, r *http.Request, logged bool) session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, err := r.Cookie(cookieName); err == nil {
		delete(a.sessions, c.Value)
	}
	now := time.Now()
	for k, s := range a.sessions {
		if now.After(s.Expires) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 4096 {
		for k := range a.sessions {
			delete(a.sessions, k)
			break
		}
	}
	token := randomToken()
	duration := 30 * time.Minute
	if logged {
		duration = 12 * time.Hour
	}
	s := session{CSRF: randomToken(), LoggedIn: logged, Expires: now.Add(duration)}
	a.sessions[token] = s
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: int(duration.Seconds())})
	return s
}
func (a *auth) validCSRF(s session, token string) bool {
	return len(token) == 64 && subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(token)) == 1
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
func (a *auth) limited(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.failures[ip]
	return v.Count >= 5 && time.Now().Before(v.Until)
}
func (a *auth) failed(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.failures {
		if now.After(v.Until) {
			delete(a.failures, k)
		}
	}
	if len(a.failures) >= 4096 {
		for k := range a.failures {
			delete(a.failures, k)
			break
		}
	}
	v := a.failures[ip]
	if now.After(v.Until) {
		v = attempts{Until: now.Add(5 * time.Minute)}
	}
	v.Count++
	a.failures[ip] = v
}
func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if sess, ok := s.auth.current(r); ok && sess.LoggedIn {
		redirect(w, r, "/admin")
		return
	}
	sess := s.auth.create(w, r, false)
	// The new cookie has not reached the request yet.
	var bData = map[string]any{"Title": "登录", "CSRF": sess.CSRF}
	s.render(w, r, "login.html", bData)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth.limited(clientIP(r)) {
		w.Header().Set("Retry-After", "300")
		s.problem(w, r, 429, "请 5 分钟后重试")
		return
	}
	if err := s.parseForm(w, r); err != nil {
		s.problem(w, r, 400, "请求无效")
		return
	}
	sess, ok := s.auth.current(r)
	if !ok || !s.auth.validCSRF(sess, r.FormValue("csrf")) {
		s.problem(w, r, 403, "请刷新登录页面")
		return
	}
	hashErr := bcrypt.CompareHashAndPassword(s.auth.hash, []byte(r.FormValue("password")))
	if hashErr != nil || subtle.ConstantTimeCompare([]byte(r.FormValue("username")), []byte(s.auth.username)) != 1 {
		s.auth.failed(clientIP(r))
		slog.Warn("login failure", "ip", clientIP(r))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(401)
		s.render(w, r, "login.html", map[string]any{"Title": "登录", "Error": "账号或密码错误"})
		return
	}
	s.auth.mu.Lock()
	delete(s.auth.failures, clientIP(r))
	s.auth.mu.Unlock()
	s.auth.create(w, r, true)
	slog.Info("login success", "ip", clientIP(r))
	redirect(w, r, "/admin")
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.mu.Lock()
	if c, err := r.Cookie(cookieName); err == nil {
		delete(s.auth.sessions, c.Value)
	}
	s.auth.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.auth.secure, SameSite: http.SameSiteLaxMode})
	redirect(w, r, "/login")
}
