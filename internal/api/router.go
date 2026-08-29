package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/filestore"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

type ctxAuth struct {
	User            store.User
	Kind, SessionID string
}
type authKey struct{}

// CurrentUser returns the authenticated user from a Gin context.
func CurrentUser(c *gin.Context) (store.User, bool) {
	v, ok := c.Get("auth_user")
	if !ok {
		return store.User{}, false
	}
	u, ok := v.(store.User)
	return u, ok
}

type limiter struct {
	mu         sync.Mutex
	users, ips map[string][]time.Time
}

func newLimiter() *limiter {
	return &limiter{users: map[string][]time.Time{}, ips: map[string][]time.Time{}}
}
func (l *limiter) blocked(u, ip string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.users[u] = prune(l.users[u], now)
	l.ips[ip] = prune(l.ips[ip], now)
	var d time.Duration
	if len(l.users[u]) >= 5 {
		d = maxWait(l.users[u], now)
	}
	if x := maxWaitIf(l.ips[ip], now, 20); x > d {
		d = x
	}
	return d, d > 0
}
func (l *limiter) fail(u, ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.users[u] = append(prune(l.users[u], now), now)
	l.ips[ip] = append(prune(l.ips[ip], now), now)
}
func (l *limiter) success(u string) { l.mu.Lock(); delete(l.users, u); l.mu.Unlock() }
func prune(v []time.Time, n time.Time) []time.Time {
	cut := n.Add(-15 * time.Minute)
	i := 0
	for i < len(v) && v[i].Before(cut) {
		i++
	}
	return v[i:]
}
func maxWait(v []time.Time, n time.Time) time.Duration { return n.Sub(v[0].Add(15 * time.Minute)) }
func maxWaitIf(v []time.Time, n time.Time, limit int) time.Duration {
	if len(v) < limit {
		return 0
	}
	return maxWait(v, n)
}

// TestHub is a reference to the active Realtime Hub, exposed for testing purposes.
var TestHub *realtime.Hub

// NewRouter constructs the M4 API router.
func NewRouter(s *store.Store, a *auth.Service, cfg config.Config) *gin.Engine {
	r := gin.New()
	r.Use(httpx.RequestID(), httpx.Recovery(slog.Default()))

	fs := filestore.New(cfg.DataDir, cfg.MaxUploadSize, s)
	StartOrphanSweeper(context.Background(), fs, slog.Default())

	b, _ := newBootstrap(s)
	if b == nil {
		b = &bootstrap{}
	}
	if b != nil && b.token != "" {
		printSetupURL(cfg.Addr, b.token)
	}
	lim := newLimiter()

	// Instantiate and run the Realtime Hub
	h := realtime.NewHub(s)
	TestHub = h
	go h.Run(context.Background())

	v1 := r.Group("/api/v1")
	v1.POST("/auth/login", login(s, a, cfg, lim))
	v1.POST("/setup", setupCreate(s, a, b))
	v1.Use(b.gate(s))
	v1.Use(RequireAuth(a, cfg))
	v1.POST("/auth/logout", sessionOnly, logout(a, cfg))
	v1.GET("/auth/me", me(cfg))
	v1.POST("/auth/password", sessionOnly, password(a, cfg))
	v1.GET("/tokens", tokenList(s))
	v1.POST("/tokens", tokenCreate(a))
	v1.DELETE("/tokens/:id", tokenDelete(s))
	v1.GET("/users", userList(s, h))
	v1.GET("/users/:id", userGet(s))
	v1.PATCH("/users/me", userMe(s))
	v1.POST("/users", RequireAdmin(), userCreate(s))
	v1.POST("/users/:id/deactivate", RequireAdmin(), userDeactivate(s))

	v1.GET("/channels", channelList(s))
	v1.POST("/channels", channelCreate(s))
	v1.GET("/channels/:id", channelGet(s))
	v1.PATCH("/channels/:id", channelUpdate(s))
	v1.POST("/channels/:id/join", channelJoin(s))
	v1.POST("/channels/:id/leave", channelLeave(s))
	v1.GET("/channels/:id/members", channelMembersList(s, h))
	v1.POST("/channels/:id/members", channelAddMembers(s))
	v1.DELETE("/channels/:id/members/:uid", channelRemoveMember(s))
	v1.GET("/channels/:id/activity", channelActivity(s))

	v1.GET("/channels/:id/messages", messageList(s))
	v1.POST("/channels/:id/messages", messageCreate(s, h))
	v1.POST("/channels/:id/read", channelRead(s, h))
	v1.GET("/messages/:id", messageGet(s))
	v1.PATCH("/messages/:id", messageUpdate(s, h))
	v1.DELETE("/messages/:id", messageDelete(s, h))
	v1.GET("/messages/:id/replies", messageListReplies(s))

	v1.GET("/dms", dmList(s, h))
	v1.POST("/dms", dmCreate(s, h))
	v1.POST("/dms/:id/hide", dmHide(s))

	v1.GET("/search", search(s))
	v1.GET("/unreads", unreadSummary(s))
	v1.GET("/presence", presence(h))

	v1.POST("/uploads", uploadFile(fs))
	v1.GET("/files/:id/:name", serveFile(fs))

	v1.GET("/ws", wsUpgrade(h, s, cfg))

	return r
}

// RequireAuth resolves a session cookie or bearer token.
func RequireAuth(a *auth.Service, cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var u store.User
		var err error
		kind := ""
		sid := ""
		if h := c.GetHeader("Authorization"); h != "" {
			parts := strings.SplitN(h, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "hm_") {
				httpx.Fail(c, 401, "unauthenticated", "Authentication required.")
				return
			}
			u, err = a.AuthenticateToken(c.Request.Context(), parts[1])
			kind = "bearer"
		} else if x, ok := c.Cookie("hm_session"); ok == nil && x != "" {
			u, _, err = a.AuthenticateSession(c.Request.Context(), x)
			sid = x
			kind = "cookie"
		} else {
			err = sql.ErrNoRows
		}
		if err != nil {
			httpx.Fail(c, 401, "unauthenticated", "Authentication required.")
			return
		}
		c.Set("auth_user", u)
		c.Set("auth_kind", kind)
		c.Set("session_id", sid)
		if kind == "cookie" && c.Request.Method != "GET" && !csrfOK(c, cfg) {
			httpx.Fail(c, 403, "csrf_failed", "Origin or Referer does not match.")
			return
		}
		c.Next()
	}
}

// RequireAdmin restricts access to administrators.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok || u.Role != "admin" {
			httpx.Fail(c, 403, "forbidden", "Administrator access required.")
			return
		}
		c.Next()
	}
}
func sessionOnly(c *gin.Context) {
	if c.GetString("auth_kind") != "cookie" {
		httpx.Fail(c, 401, "session_required", "A session cookie is required.")
		return
	}
	c.Next()
}
func csrfOK(c *gin.Context, cfg config.Config) bool {
	want := cfg.BaseURL
	if want == "" {
		scheme := "http"
		if c.Request.TLS != nil || (cfg.BehindProxy && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")) {
			scheme = "https"
		}
		want = scheme + "://" + c.Request.Host
	}
	for _, h := range []string{"Origin", "Referer"} {
		x := c.GetHeader(h)
		if x == "" {
			continue
		}
		u, err := url.Parse(x)
		if err == nil && strings.EqualFold(u.Scheme+"://"+u.Host, want) {
			return true
		}
		return false
	}
	return false
}

func login(s *store.Store, a *auth.Service, cfg config.Config, l *limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			Login    string `json:"login"`
			Password string `json:"password"`
		}
		if c.ShouldBindJSON(&in) != nil || in.Login == "" {
			httpx.Fail(c, 400, "invalid_request", "Login and password are required.")
			return
		}
		ip := clientIP(c, cfg)
		now := time.Now()
		if d, ok := l.blocked(strings.ToLower(in.Login), ip, now); ok {
			c.Header("Retry-After", strconv.Itoa(int(d.Seconds())+1))
			httpx.Fail(c, 429, "rate_limited", "Too many login failures.")
			return
		}
		u, err := s.GetUserByLogin(c.Request.Context(), in.Login)
		valid := false
		if err == nil {
			valid = auth.CheckPassword(u.PasswordHash, in.Password)
		} else {
			valid = auth.CheckPassword(dummyHashForTiming(), in.Password)
		}
		if err != nil || !valid || u.Status != "active" {
			l.fail(strings.ToLower(in.Login), ip, now)
			httpx.Fail(c, 401, "unauthenticated", "Invalid login or password.")
			return
		}
		l.success(strings.ToLower(in.Login))
		sid, err := a.CreateSession(c.Request.Context(), u.ID, c.GetHeader("User-Agent"), ip)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not create session.")
			return
		}
		secure := cfg.BehindProxy || cfg.TLS.Cert != ""
		http.SetCookie(c.Writer, &http.Cookie{Name: "hm_session", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure, MaxAge: int(a.SessionTTL.Seconds())})
		c.JSON(200, gin.H{"user": publicUser(u)})
	}
}
func dummyHashForTiming() string { return auth.DummyHash() }
func logout(a *auth.Service, cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := a.Store.DeleteSession(c.Request.Context(), c.GetString("session_id")); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not log out.")
			return
		}
		clearCookie(c, cfg)
		c.Status(204)
	}
}
func me(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _ := CurrentUser(c)
		c.JSON(200, gin.H{"user": publicUser(u), "workspace": gin.H{"name": cfg.WorkspaceName}})
	}
}
func password(a *auth.Service, cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _ := CurrentUser(c)
		var in struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if c.ShouldBindJSON(&in) != nil {
			httpx.Fail(c, 400, "invalid_request", "Current and new passwords are required.")
			return
		}
		if err := a.ChangePassword(c.Request.Context(), u, in.Current, in.New); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 401, "invalid_password", "Current password is incorrect.")
			} else {
				httpx.Fail(c, 400, "invalid_password", err.Error())
			}
			return
		}
		clearCookie(c, cfg)
		c.Status(204)
	}
}
func tokenList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _ := CurrentUser(c)
		v, err := s.ListAPITokens(c.Request.Context(), u.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list tokens.")
			return
		}
		data := make([]gin.H, 0, len(v))
		for _, t := range v {
			item := gin.H{"id": strconv.FormatInt(t.ID, 10), "name": t.Name, "created_at": t.CreatedAt}
			if t.ExpiresAt != 0 {
				item["expires_at"] = t.ExpiresAt
			}
			if t.LastUsedAt != 0 {
				item["last_used_at"] = t.LastUsedAt
			}
			data = append(data, item)
		}
		c.JSON(200, gin.H{"data": data})
	}
}
func tokenCreate(a *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _ := CurrentUser(c)
		var in struct {
			Name      string `json:"name"`
			ExpiresIn string `json:"expires_in"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request payload: "+err.Error())
			return
		}
		if in.Name == "" {
			httpx.Fail(c, 400, "invalid_request", "Token name is required.")
			return
		}
		var d time.Duration
		var err error
		if in.ExpiresIn != "" {
			d, err = time.ParseDuration(in.ExpiresIn)
			if err != nil || d <= 0 {
				httpx.FailField(c, 400, "invalid_request", "expires_in must be a positive duration.", "expires_in")
				return
			}
		}
		id, plain, err := a.CreateToken(c.Request.Context(), u.ID, in.Name, d)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not create token.")
			return
		}
		c.JSON(201, gin.H{"id": strconv.FormatInt(id, 10), "name": in.Name, "token": plain})
	}
}
func tokenDelete(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _ := CurrentUser(c)
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "not_found", "Token not found.")
			return
		}
		if err := s.DeleteAPIToken(c.Request.Context(), u.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "not_found", "Token not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not delete token.")
			return
		}
		c.Status(204)
	}
}
func clearCookie(c *gin.Context, cfg config.Config) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "hm_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: cfg.BehindProxy || cfg.TLS.Cert != "", MaxAge: -1})
}
func publicUser(u store.User) gin.H {
	return gin.H{"id": strconv.FormatInt(u.ID, 10), "username": u.Username, "email": u.Email, "display_name": u.DisplayName, "avatar_color": u.AvatarColor, "avatar_url": u.AvatarURL, "role": u.Role, "is_bot": u.IsBot, "status": u.Status, "created_at": u.CreatedAt}
}

// publicUserOnline is publicUser plus a live "online" flag sourced from the Hub's connection
// registry — used wherever a user list needs a presence dot (GET /users, DM peers/members,
// channel member lists).
func publicUserOnline(u store.User, h *realtime.Hub) gin.H {
	m := publicUser(u)
	m["online"] = h.IsOnline(u.ID)
	return m
}
func clientIP(c *gin.Context, cfg config.Config) string {
	if cfg.BehindProxy {
		p := strings.Split(c.GetHeader("X-Forwarded-For"), ",")
		if len(p) > 0 && strings.TrimSpace(p[len(p)-1]) != "" {
			return strings.TrimSpace(p[len(p)-1])
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil {
		return host
	}
	return c.Request.RemoteAddr
}
