package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const adminCookieName = "gostream3d_admin"
const adminSessionLifetime = 12 * time.Hour

type loginAttempt struct {
	count int
	until time.Time
}

type adminAuth struct {
	username          string
	credentialsPath   string
	savedPasswordHash []byte
	usernameHash      [32]byte
	passwordHash      [32]byte
	legacyKey         string
	mu                sync.Mutex
	sessions          map[string]time.Time
	attempts          map[string]loginAttempt
}

func newAdminAuth(username, password, legacyKey string) *adminAuth {
	if username == "" && legacyKey != "" {
		username, password = "admin", legacyKey
	}
	return &adminAuth{
		username:     username,
		usernameHash: sha256.Sum256([]byte(username)),
		passwordHash: sha256.Sum256([]byte(password)),
		legacyKey:    legacyKey,
		sessions:     make(map[string]time.Time),
		attempts:     make(map[string]loginAttempt),
	}
}

func (a *adminAuth) login(c *gin.Context) {
	if !sameAdminOrigin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "invalid origin"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid login request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid login request"})
		return
	}
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}
	now := time.Now()
	a.mu.Lock()
	attempt := a.attempts[ip]
	if now.Before(attempt.until) && attempt.count >= 5 {
		a.mu.Unlock()
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "登录尝试过多，请一分钟后重试。"})
		return
	}
	if !now.Before(attempt.until) {
		attempt = loginAttempt{until: now.Add(time.Minute)}
	}
	usernameHash := sha256.Sum256([]byte(input.Username))
	valid := subtle.ConstantTimeCompare(usernameHash[:], a.usernameHash[:]) == 1 &&
		a.passwordMatchesLocked(input.Password)
	if !valid {
		attempt.count++
		a.attempts[ip] = attempt
		a.mu.Unlock()
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "账号或密码错误。"})
		return
	}
	delete(a.attempts, ip)
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		a.mu.Unlock()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "无法创建登录会话。"})
		return
	}
	for token, expires := range a.sessions {
		if !now.Before(expires) {
			delete(a.sessions, token)
		}
	}
	if len(a.sessions) >= 1000 {
		a.mu.Unlock()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "登录会话已满，请稍后重试。"})
		return
	}
	token := hex.EncodeToString(random[:])
	a.sessions[token] = now.Add(adminSessionLifetime)
	a.mu.Unlock()
	http.SetCookie(c.Writer, &http.Cookie{
		Name: adminCookieName, Value: token, Path: "/api/v1", MaxAge: int(adminSessionLifetime.Seconds()),
		HttpOnly: true, Secure: secureAdminCookie(c.Request), SameSite: http.SameSiteStrictMode,
	})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
}

func (a *adminAuth) logout(c *gin.Context) {
	if !sameAdminOrigin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "invalid origin"})
		return
	}
	if cookie, err := c.Request.Cookie(adminCookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: adminCookieName, Path: "/api/v1", MaxAge: -1,
		HttpOnly: true, Secure: secureAdminCookie(c.Request), SameSite: http.SameSiteStrictMode,
	})
	c.Status(http.StatusNoContent)
}

func (a *adminAuth) required(c *gin.Context) {
	if !sameAdminOrigin(c.Request) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "invalid origin"})
		return
	}
	a.mu.Lock()
	ok := a.authorizedLocked(c)
	a.mu.Unlock()
	if ok {
		c.Next()
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
}

func (a *adminAuth) authorizedLocked(c *gin.Context) bool {
	if a.legacyKey != "" {
		if provided, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok &&
			subtle.ConstantTimeCompare([]byte(provided), []byte(a.legacyKey)) == 1 {
			return true
		}
	}
	cookie, err := c.Request.Cookie(adminCookieName)
	if err == nil {
		expires, ok := a.sessions[cookie.Value]
		if ok && !time.Now().Before(expires) {
			delete(a.sessions, cookie.Value)
			ok = false
		}
		if ok {
			return true
		}
	}
	return false
}

func (a *adminAuth) passwordMatchesLocked(password string) bool {
	if a.savedPasswordHash != nil {
		return len(password) <= 72 && bcrypt.CompareHashAndPassword(a.savedPasswordHash, []byte(password)) == nil
	}
	hash := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(hash[:], a.passwordHash[:]) == 1
}

func secureAdminCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func sameAdminOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" {
		return false
	}
	expectedScheme := "http"
	if secureAdminCookie(r) {
		expectedScheme = "https"
	}
	if u.Scheme != expectedScheme {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) || strings.EqualFold(u.Host, r.Header.Get("X-Forwarded-Host"))
}
