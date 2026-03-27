package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"github.com/patrickmn/go-cache"
)

// JWT 配置
const (
	AccessTokenExpiry  = 15 * time.Minute   // Access Token 有效期
	RefreshTokenExpiry = 7 * 24 * time.Hour // Refresh Token 有效期
)

var (
	jwtSecret        = []byte("your-secret-key-change-this-in-production")
	refreshTokenCache = cache.New(RefreshTokenExpiry, 10*time.Minute)
	blacklistCache    = cache.New(RefreshTokenExpiry, 10*time.Minute)
)

// TokenClaims JWT 声明
type TokenClaims struct {
	UserID   int    `json:"user_id"`
	Nickname string `json:"nickname"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

// TokenPair 双Token结构
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// ==================== 密码加密 ====================

// HashPassword 使用 bcrypt 加密密码
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword 验证密码
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ==================== JWT Token 操作 ====================

// GenerateTokenPair 生成双Token
func GenerateTokenPair(userID int, nickname string) (*TokenPair, error) {
	// 生成 Access Token
	accessClaims := TokenClaims{
		UserID:    userID,
		Nickname:  nickname,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(jwtSecret)
	if err != nil {
		return nil, err
	}

	// 生成 Refresh Token
	refreshTokenBytes := make([]byte, 32)
	if _, err := rand.Read(refreshTokenBytes); err != nil {
		return nil, err
	}
	refreshTokenString := base64.URLEncoding.EncodeToString(refreshTokenBytes)

	// 存储 Refresh Token 到缓存
	refreshTokenCache.Set(refreshTokenString, userID, RefreshTokenExpiry)

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresIn:    int64(AccessTokenExpiry.Seconds()),
	}, nil
}

// ParseAccessToken 解析 Access Token
func ParseAccessToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*TokenClaims); ok && token.Valid {
		if claims.TokenType != "access" {
			return nil, fmt.Errorf("invalid token type")
		}
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ValidateRefreshToken 验证 Refresh Token
func ValidateRefreshToken(refreshToken string) (int, bool) {
	// 检查是否在黑名单中
	if _, found := blacklistCache.Get(refreshToken); found {
		return 0, false
	}

	// 检查是否存在
	if userID, found := refreshTokenCache.Get(refreshToken); found {
		return userID.(int), true
	}

	return 0, false
}

// BlacklistRefreshToken 将 Refresh Token 加入黑名单
func BlacklistRefreshToken(refreshToken string) {
	blacklistCache.Set(refreshToken, true, RefreshTokenExpiry)
	refreshTokenCache.Delete(refreshToken)
}

// RefreshAccessToken 使用 Refresh Token 刷新 Access Token
func RefreshAccessToken(refreshToken string) (*TokenPair, error) {
	userID, valid := ValidateRefreshToken(refreshToken)
	if !valid {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// 获取用户信息
	user, err := GetUserByID(userID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// 使旧的 Refresh Token 失效
	BlacklistRefreshToken(refreshToken)

	// 生成新的 Token Pair
	return GenerateTokenPair(userID, user.Nickname)
}

// ==================== 限流器 ====================

// RateLimiter 基础限流器
type RateLimiter struct {
	requests map[string][]time.Time
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// 清理过期的请求记录
	var validRequests []time.Time
	for _, t := range rl.requests[key] {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	// 检查是否超过限制
	if len(validRequests) >= rl.limit {
		rl.requests[key] = validRequests
		return false
	}

	// 添加新请求
	validRequests = append(validRequests, now)
	rl.requests[key] = validRequests

	return true
}

// AllowWithCount 返回是否允许以及剩余次数
func (rl *RateLimiter) AllowWithCount(key string) (bool, int, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// 清理过期的请求记录
	var validRequests []time.Time
	var oldestTime time.Time
	for _, t := range rl.requests[key] {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
			if oldestTime.IsZero() || t.Before(oldestTime) {
				oldestTime = t
			}
		}
	}

	// 检查是否超过限制
	if len(validRequests) >= rl.limit {
		rl.requests[key] = validRequests
		// 计算还需要等待多久
		waitTime := rl.window - now.Sub(oldestTime)
		if waitTime < 0 {
			waitTime = rl.window
		}
		return false, 0, waitTime
	}

	// 添加新请求
	validRequests = append(validRequests, now)
	rl.requests[key] = validRequests

	remaining := rl.limit - len(validRequests)
	return true, remaining, 0
}

// SuccessLimiter 成功才计数的限流器（用于注册）
type SuccessLimiter struct {
	requests map[string][]time.Time
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

func NewSuccessLimiter(limit int, window time.Duration) *SuccessLimiter {
	return &SuccessLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Check 检查是否超过限制，不记录请求
func (sl *SuccessLimiter) Check(key string) (bool, int) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sl.window)

	// 清理过期的请求记录
	var validRequests []time.Time
	for _, t := range sl.requests[key] {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}
	sl.requests[key] = validRequests

	remaining := sl.limit - len(validRequests)
	if remaining < 0 {
		remaining = 0
	}

	return len(validRequests) < sl.limit, remaining
}

// RecordSuccess 记录一次成功请求
func (sl *SuccessLimiter) RecordSuccess(key string) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	now := time.Now()
	sl.requests[key] = append(sl.requests[key], now)
}

// 全局限流器实例
var (
	loginLimiter    = NewRateLimiter(3, 1*time.Minute)      // 登录：每IP每分钟3次
	registerLimiter = NewSuccessLimiter(3, 1*time.Minute)   // 注册：每IP每分钟3次（仅成功时计数）
)

// ==================== HTTP 中间件 ====================

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		claims, err := ParseAccessToken(parts[1])
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// 将用户信息存入请求上下文
		ctx := r.Context()
		r = r.WithContext(ctx)
		r.Header.Set("X-User-ID", fmt.Sprintf("%d", claims.UserID))
		r.Header.Set("X-User-Nickname", claims.Nickname)

		next(w, r)
	}
}

// RateLimitMiddleware 限流中间件
func RateLimitMiddleware(limiter *RateLimiter, keyFunc func(r *http.Request) string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if !limiter.Allow(key) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next(w, r)
		}
	}
}

// GetClientIP 获取客户端 IP
func GetClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		return strings.Split(xff, ",")[0]
	}
	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		return xri
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}
