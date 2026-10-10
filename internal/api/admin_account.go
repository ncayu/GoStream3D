package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func (a *adminAuth) restoreCredentials(path string) error {
	saved, err := loadAdminCredentials(path)
	if err != nil {
		return err
	}
	a.credentialsPath = path
	if saved != nil {
		a.username = saved.Username
		a.usernameHash = sha256.Sum256([]byte(saved.Username))
		a.savedPasswordHash = []byte(saved.PasswordHash)
		a.passwordHash = [32]byte{}
		a.legacyKey = ""
	}
	return nil
}

func (a *adminAuth) me(c *gin.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"code": 0, "username": a.username, "account_update_supported": a.credentialsPath != ""})
}

func (a *adminAuth) updateAccount(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		CurrentPassword string `json:"current_password"`
		Username        string `json:"username"`
		NewPassword     string `json:"new_password"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请填写当前密码、账号和新密码。"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || !validAdminUsername(input.Username) || len(input.CurrentPassword) == 0 || len(input.NewPassword) < 12 || len(input.NewPassword) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "账号为 1–64 个字符，不含空白；新密码为 12–72 字节。"})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Recheck under the rotation lock: another update may have revoked this session.
	if !a.authorizedLocked(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "登录已失效，请重新登录。"})
		return
	}
	if a.credentialsPath == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "当前服务未配置账号存储，暂时无法修改。"})
		return
	}
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}
	now := time.Now()
	attempt := a.attempts[ip]
	if now.Before(attempt.until) && attempt.count >= 5 {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "密码验证过多，请一分钟后重试。"})
		return
	}
	if !a.passwordMatchesLocked(input.CurrentPassword) {
		if !now.Before(attempt.until) {
			attempt = loginAttempt{until: now.Add(time.Minute)}
		}
		attempt.count++
		a.attempts[ip] = attempt
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "当前密码错误。"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err == nil {
		err = saveAdminCredentials(a.credentialsPath, adminCredentials{Version: 1, Username: input.Username, PasswordHash: string(hash)})
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "账号保存失败，请检查数据目录是否可写，原账号保持有效。"})
		return
	}
	a.username = input.Username
	a.usernameHash = sha256.Sum256([]byte(input.Username))
	a.savedPasswordHash = hash
	a.passwordHash = [32]byte{}
	a.legacyKey = ""
	a.sessions = make(map[string]time.Time)
	a.attempts = make(map[string]loginAttempt)
	http.SetCookie(c.Writer, &http.Cookie{Name: adminCookieName, Path: "/api/v1", MaxAge: -1, HttpOnly: true, Secure: secureAdminCookie(c.Request), SameSite: http.SameSiteStrictMode})
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "账号密码已修改，请使用新账号重新登录。"})
}
