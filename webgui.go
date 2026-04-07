package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type WebGUIGame struct {
	game              *Game
	mu                sync.Mutex
	actionChan        chan ActionData
	firstGame         bool
	currentPhase      string
	currentPlayerName string
	waitingForInput   bool
	canCheck          bool
	canCall           bool
	canRaise          bool
	minRaise          int
	maxRaise          int
	callAmount        int
	message           string
	gameOver          bool
	winnerText        string
	askContinue       bool
	askShuffle        bool
	showdownCards     []string
	winnerHandRank    string
	winnerName        string
	currentUser       *User
	noChips           bool
	gameMode          string
	aiPlayerCount     int
}

type GameState struct {
	Players         []PlayerState `json:"players"`
	CommunityCards  []string      `json:"communityCards"`
	Pot             int           `json:"pot"`
	Phase           string        `json:"phase"`
	CurrentPlayer   string        `json:"currentPlayer"`
	Message         string        `json:"message"`
	GameOver        bool          `json:"gameOver"`
	WinnerText      string        `json:"winnerText"`
	CanCheck        bool          `json:"canCheck"`
	CanCall         bool          `json:"canCall"`
	CanRaise        bool          `json:"canRaise"`
	MinRaise        int           `json:"minRaise"`
	MaxRaise        int           `json:"maxRaise"`
	CallAmount      int           `json:"callAmount"`
	WaitingForInput bool          `json:"waitingForInput"`
	AskContinue     bool          `json:"askContinue"`
	AskShuffle      bool          `json:"askShuffle"`
	ShowdownCards   []string      `json:"showdownCards"`
	WinnerHandRank  string        `json:"winnerHandRank"`
	WinnerName      string        `json:"winnerName"`
	NoChips         bool          `json:"noChips"`
	GameMode        string        `json:"gameMode"`
	RoomID          string        `json:"roomId"`
}

type ActionData struct {
	Action Action
	Amount int
}

type PlayerState struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Chips        int      `json:"chips"`
	Cards        []string `json:"cards"`
	Bet          int      `json:"bet"`
	Folded       bool     `json:"folded"`
	AllIn        bool     `json:"allIn"`
	IsHuman      bool     `json:"isHuman"`
	IsCurrent    bool     `json:"isCurrent"`
	IsDealer     bool     `json:"isDealer"`
	IsSmallBlind bool     `json:"isSmallBlind"`
	IsBigBlind   bool     `json:"isBigBlind"`
}

func NewWebGUIGame() *WebGUIGame {
	return &WebGUIGame{
		actionChan:    make(chan ActionData, 1),
		firstGame:     true,
		gameMode:      "ai",
		aiPlayerCount: 6,
	}
}

// 内存用户存储（当数据库不可用时使用）
var (
	users      = make(map[string]*User)
	usersByID  = make(map[int]*User)
	nextUserID = 1
	usersMu    sync.Mutex
)

func (g *WebGUIGame) Run() {
	// 初始化WebSocket
	InitWebSocket()

	// 主页 - 需要JWT认证
	http.HandleFunc("/", g.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		g.handleIndex(w, r)
	}))

	// 登录注册页面 - 不需要认证
	http.HandleFunc("/login", g.handleLoginPage)
	http.HandleFunc("/register", g.handleRegisterPage)

	// 房间相关页面
	http.HandleFunc("/room/create", g.authMiddleware(g.handleCreateRoomPage))
	http.HandleFunc("/room/join", g.authMiddleware(g.handleJoinRoomPage))
	http.HandleFunc("/room/waiting", g.authMiddleware(g.handleWaitingRoomPage))
	http.HandleFunc("/room/game", g.authMiddleware(g.handleRoomGamePage))

	// API接口
	http.HandleFunc("/api/captcha", HandleCaptcha)
	http.HandleFunc("/api/login", g.handleLoginAPI)
	http.HandleFunc("/api/register", g.handleRegisterAPI)
	http.HandleFunc("/api/logout", g.authMiddleware(g.handleLogoutAPI))
	http.HandleFunc("/api/refresh", g.handleRefreshToken)

	// 房间相关API
	http.HandleFunc("/api/room/create", g.authMiddleware(HandleCreateRoom))
	http.HandleFunc("/api/room/join", g.authMiddleware(HandleJoinRoom))
	http.HandleFunc("/api/room/leave", g.authMiddleware(HandleLeaveRoom))
	http.HandleFunc("/api/room/return", g.authMiddleware(HandleReturnToRoom))
	http.HandleFunc("/api/room/get", g.authMiddleware(HandleGetRoom))
	http.HandleFunc("/api/room/my", g.authMiddleware(HandleGetMyRoom))
	http.HandleFunc("/api/room/check_reconnect", g.authMiddleware(HandleCheckReconnect))
	http.HandleFunc("/api/room/ready", g.authMiddleware(HandleSetReady))
	http.HandleFunc("/api/room/start", g.authMiddleware(HandleStartGame))
	http.HandleFunc("/api/room/kick", g.authMiddleware(HandleKickPlayer))

	// WebSocket端点
	http.HandleFunc("/ws", g.authMiddleware(HandleWebSocket))

	// 游戏相关API - 需要认证
	http.HandleFunc("/state", g.authMiddleware(g.handleState))
	http.HandleFunc("/action", g.authMiddleware(g.handleAction))

	// 静态资源
	http.Handle("/puke-img/", http.StripPrefix("/puke-img/", http.FileServer(http.Dir("puke-img"))))
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/bm.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "bm.png")
	})
	http.HandleFunc("/zhuomian.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "zhuomian.png")
	})

	port := "8080"
	fmt.Println("=====================================")
	fmt.Println("   德州扑克 Web GUI 已启动！")
	fmt.Println("=====================================")
	fmt.Printf("请在浏览器中打开: http://192.168.0.31:%s\n", port)
	fmt.Println("=====================================")

	if err := http.ListenAndServe("192.168.0.31:"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// authMiddleware JWT认证中间件（支持无感刷新）
func (g *WebGUIGame) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 从Cookie或Header获取token
		var tokenString string

		// 先尝试从Cookie获取
		if cookie, err := r.Cookie("access_token"); err == nil {
			tokenString = cookie.Value
		}

		// 如果没有，尝试从Authorization Header获取
		if tokenString == "" {
			authHeader := r.Header.Get("Authorization")
			if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				tokenString = authHeader[7:]
			}
		}

		// 如果没有token，尝试用refresh token刷新
		if tokenString == "" {
			if g.tryRefreshToken(w, r) {
				// 刷新成功，重新执行请求
				next(w, r)
				return
			}
			// 刷新失败，需要登录
			if (len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api") || r.URL.Path == "/state" || r.URL.Path == "/action" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "未登录或登录已过期",
				})
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		claims, err := ParseAccessToken(tokenString)
		if err != nil {
			// Access Token无效或过期，尝试用refresh token刷新
			if g.tryRefreshToken(w, r) {
				// 刷新成功，重新执行请求
				next(w, r)
				return
			}
			// 刷新失败
			if (len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api") || r.URL.Path == "/state" || r.URL.Path == "/action" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "登录已过期，请重新登录",
				})
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Token有效，检查是否需要预刷新（即将过期）
		shouldRefresh, _, err := ShouldRefreshToken(tokenString)
		if err == nil && shouldRefresh {
			// Token即将过期，在后台静默刷新
			go g.tryRefreshToken(nil, r)
		}

		// 获取用户信息
		user, err := GetUserByID(claims.UserID)
		if err != nil || user == nil {
			if r.URL.Path[:4] == "/api" || r.URL.Path == "/state" || r.URL.Path == "/action" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "用户不存在",
				})
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// 将用户信息存入上下文
		g.mu.Lock()
		g.currentUser = user
		g.mu.Unlock()

		next(w, r)
	}
}

// tryRefreshToken 尝试使用Refresh Token刷新Access Token
// 如果w为nil，则只刷新不设置cookie（后台静默刷新）
func (g *WebGUIGame) tryRefreshToken(w http.ResponseWriter, r *http.Request) bool {
	// 获取refresh token
	var refreshToken string
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		refreshToken = cookie.Value
	}

	if refreshToken == "" {
		return false
	}

	// 刷新token
	tokenPair, err := RefreshAccessToken(refreshToken)
	if err != nil {
		return false
	}

	// 如果w不为nil，设置新Cookie
	if w != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "access_token",
			Value:    tokenPair.AccessToken,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   int(AccessTokenExpiry.Seconds()),
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    tokenPair.RefreshToken,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   int(RefreshTokenExpiry.Seconds()),
		})
	}

	return true
}

// rateLimitMiddleware 限流中间件
func (g *WebGUIGame) rateLimitMiddleware(limiter *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := GetClientIP(r)
		if !limiter.Allow(clientIP) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "请求过于频繁，请稍后再试",
			})
			return
		}
		next(w, r)
	}
}

func (g *WebGUIGame) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// 检查是否已登录
	if cookie, err := r.Cookie("access_token"); err == nil {
		if _, err := ParseAccessToken(cookie.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	// 检查是否已登录
	if cookie, err := r.Cookie("access_token"); err == nil {
		if _, err := ParseAccessToken(cookie.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	tmpl, err := template.ParseFiles("templates/register.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

// handleCreateRoomPage 创建房间页面
func (g *WebGUIGame) handleCreateRoomPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/create_room.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

// handleJoinRoomPage 加入房间页面
func (g *WebGUIGame) handleJoinRoomPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/join_room.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

// handleWaitingRoomPage 房间等待页面
func (g *WebGUIGame) handleWaitingRoomPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/waiting_room.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

// handleRoomGamePage 房间游戏页面（多人对战）
func (g *WebGUIGame) handleRoomGamePage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/room_game.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleLoginAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Captcha  string `json:"captcha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求",
		})
		return
	}

	// 验证验证码
	clientIP := GetClientIP(r)
	if !VerifyCaptcha(clientIP, req.Captcha) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "验证码错误或已过期",
		})
		return
	}

	// 检查登录限流
	allowed, remaining, waitTime := loginLimiter.AllowWithCount(clientIP)
	if !allowed {
		minutes := int(waitTime.Seconds()) / 60
		seconds := int(waitTime.Seconds()) % 60
		var waitStr string
		if minutes > 0 {
			waitStr = fmt.Sprintf("%d分%d秒", minutes, seconds)
		} else {
			waitStr = fmt.Sprintf("%d秒", seconds)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("登录次数过多，请等待%s后再试", waitStr),
		})
		return
	}

	var user *User
	var err error

	if db != nil {
		user, err = GetUserByNickname(req.Username)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if user == nil {
			user, err = GetUserByEmail(req.Username)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "数据库错误",
				})
				return
			}
		}
	} else {
		usersMu.Lock()
		defer usersMu.Unlock()

		if u, ok := users[req.Username]; ok && CheckPassword(req.Password, u.Password) {
			user = u
		} else {
			for _, u := range users {
				if u.Email == req.Username && CheckPassword(req.Password, u.Password) {
					user = u
					break
				}
			}
		}
	}

	if user == nil || !CheckPassword(req.Password, user.Password) {
		// 登录失败，显示剩余次数
		var message string
		if remaining > 0 {
			message = fmt.Sprintf("账号或密码错误，剩余尝试次数：%d", remaining)
		} else {
			message = "账号或密码错误，本次为最后一次尝试机会"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": message,
		})
		return
	}

	// 生成双Token
	tokenPair, err := GenerateTokenPair(user.ID, user.Nickname)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "生成Token失败",
		})
		return
	}

	// 设置Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    tokenPair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(AccessTokenExpiry.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokenPair.RefreshToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(RefreshTokenExpiry.Seconds()),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "登录成功",
	})
}

func (g *WebGUIGame) handleRegisterAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Captcha  string `json:"captcha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求",
		})
		return
	}

	// 验证验证码
	clientIP := GetClientIP(r)
	if !VerifyCaptcha(clientIP, req.Captcha) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "验证码错误或已过期",
		})
		return
	}

	// 检查注册限流（仅成功时计数，但先检查是否超过限制）
	allowed, remaining := registerLimiter.Check(clientIP)
	if !allowed {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "注册次数过多，请稍后再试",
		})
		return
	}

	// 密码加密
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "密码加密失败",
		})
		return
	}

	if db != nil {
		exists, err := CheckNicknameExists(req.Nickname)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "昵称已存在",
			})
			return
		}

		exists, err = CheckEmailExists(req.Email)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "邮箱已被注册",
			})
			return
		}

		_, err = CreateUser(req.Nickname, req.Email, hashedPassword)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "创建用户失败",
			})
			return
		}
	} else {
		usersMu.Lock()
		defer usersMu.Unlock()

		if _, ok := users[req.Nickname]; ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "昵称已存在",
			})
			return
		}

		for _, u := range users {
			if u.Email == req.Email {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "邮箱已被注册",
				})
				return
			}
		}

		user := &User{
			ID:       nextUserID,
			Nickname: req.Nickname,
			Email:    req.Email,
			Password: hashedPassword,
		}
		nextUserID++
		users[req.Nickname] = user
		usersByID[user.ID] = user
	}

	// 注册成功，记录到限流器
	registerLimiter.RecordSuccess(clientIP)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("注册成功，今日还可注册%d个账号", remaining-1),
	})
}

func (g *WebGUIGame) handleLogoutAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 获取refresh token并加入黑名单
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		BlacklistRefreshToken(cookie.Value)
	}

	// 清除Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "登出成功",
	})
}

func (g *WebGUIGame) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 获取refresh token
	var refreshToken string
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		refreshToken = cookie.Value
	}

	if refreshToken == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未提供Refresh Token",
		})
		return
	}

	// 刷新token
	tokenPair, err := RefreshAccessToken(refreshToken)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Refresh Token无效或已过期",
		})
		return
	}

	// 设置新Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    tokenPair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(AccessTokenExpiry.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokenPair.RefreshToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(RefreshTokenExpiry.Seconds()),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Token刷新成功",
	})
}

func (g *WebGUIGame) handleState(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var state *GameState
	if g.game == nil {
		state = &GameState{
			Message: "",
		}
	} else {
		state = g.getGameState()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (g *WebGUIGame) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var actionReq struct {
		Action string `json:"action"`
		Amount int    `json:"amount"`
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&actionReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var action Action
	switch actionReq.Action {
	case "fold":
		action = Fold
	case "check":
		action = Check
	case "call":
		action = Call
	case "raise":
		action = Raise
	case "allin":
		action = AllIn
	case "start":
		g.mu.Lock()
		g.game = nil
		g.winnerText = ""
		g.winnerHandRank = ""
		g.winnerName = ""
		g.showdownCards = nil
		g.gameOver = false
		g.askContinue = false
		g.askShuffle = false
		g.noChips = false
		g.message = ""
		g.currentPhase = ""
		g.currentPlayerName = ""
		g.waitingForInput = false
		g.mu.Unlock()

		g.mu.Lock()
		rand.Seed(time.Now().UnixNano())

		// 检查是否是房间模式
		if actionReq.RoomID != "" {
			// 房间模式：从数据库获取玩家列表
			players, err := GetRoomPlayers(actionReq.RoomID)
			if err != nil || len(players) < 2 {
				g.mu.Unlock()
				http.Error(w, "房间玩家不足", http.StatusBadRequest)
				return
			}

			// 创建游戏，使用房间中的玩家
			playerNames := make([]string, len(players))
			for i, p := range players {
				playerNames[i] = p.Nickname
			}

			// 找到当前用户在玩家列表中的位置
			humanIndex := -1
			for i, p := range players {
				if g.currentUser != nil && p.UserID == g.currentUser.ID {
					humanIndex = i
					break
				}
			}

			g.game = NewGameWithPlayers(playerNames, humanIndex)
			g.gameMode = "room"
		} else {
			// AI模式
			numPlayers := g.aiPlayerCount
			if actionReq.Amount >= 2 && actionReq.Amount <= 10 {
				numPlayers = actionReq.Amount
			}

			humanName := ""
			if g.currentUser != nil {
				humanName = g.currentUser.Nickname
			}
			g.game = NewGame(numPlayers, 100, humanName)
			g.gameMode = "ai"
		}

		g.firstGame = true
		go g.playHand()
		g.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	case "set_ai_count":
		if actionReq.Amount >= 2 && actionReq.Amount <= 10 {
			g.mu.Lock()
			g.aiPlayerCount = actionReq.Amount
			g.gameMode = "ai"
			g.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		return
	case "continue":
		go func() {
			g.actionChan <- ActionData{Action: Check, Amount: -1}
		}()
		w.WriteHeader(http.StatusOK)
		return
	case "shuffle":
		g.mu.Lock()
		g.shufflePlayers()
		g.mu.Unlock()
		go func() {
			g.actionChan <- ActionData{Action: Check, Amount: -2}
		}()
		w.WriteHeader(http.StatusOK)
		return
	case "noContinue":
		w.WriteHeader(http.StatusOK)
		return
	case "timeout":
		action = Fold
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	g.actionChan <- ActionData{Action: action, Amount: actionReq.Amount}
	w.WriteHeader(http.StatusOK)
}

// 其他辅助函数...
func (g *WebGUIGame) shufflePlayers() {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(g.game.Players), func(i, j int) {
		g.game.Players[i], g.game.Players[j] = g.game.Players[j], g.game.Players[i]
	})
	for i, p := range g.game.Players {
		p.ID = i + 1
	}
}

func (g *WebGUIGame) shouldSkipBettingRounds() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	activePlayers := g.game.getActivePlayers()
	for _, p := range activePlayers {
		if !p.AllIn {
			return false
		}
	}
	return len(activePlayers) > 1
}

func (g *WebGUIGame) playHand() {
	g.mu.Lock()

	g.game.NewHand()
	g.currentPhase = "翻牌前"
	g.message = "游戏开始！"
	g.gameOver = false
	g.winnerText = ""
	g.askContinue = false
	g.askShuffle = false
	g.showdownCards = nil
	g.winnerHandRank = ""
	g.winnerName = ""

	g.game.DealHoleCards()
	g.currentPhase = "翻牌前"
	g.mu.Unlock()

	time.Sleep(500 * time.Millisecond)

	g.mu.Lock()
	g.game.PostBlinds()
	g.currentPhase = "翻牌前"
	g.mu.Unlock()

	g.runBettingRound("翻牌前")

	g.mu.Lock()
	if g.game.GameOver() {
		g.mu.Unlock()
		g.showdown()
		return
	}
	g.mu.Unlock()

	if g.shouldSkipBettingRounds() {
		g.mu.Lock()
		g.message = "所有玩家全下，直接发牌至河牌！"
		g.mu.Unlock()
		time.Sleep(1000 * time.Millisecond)
	} else {
		g.mu.Lock()
		g.game.DealFlop()
		g.currentPhase = "翻牌圈"
		g.message = ""
		g.mu.Unlock()

		g.runBettingRound("翻牌圈")

		g.mu.Lock()
		if g.game.GameOver() {
			g.mu.Unlock()
			g.showdown()
			return
		}
		g.mu.Unlock()

		if g.shouldSkipBettingRounds() {
			g.mu.Lock()
			g.message = "所有玩家全下，直接发牌至河牌！"
			g.mu.Unlock()
			time.Sleep(1000 * time.Millisecond)
		} else {
			g.mu.Lock()
			g.game.DealTurn()
			g.currentPhase = "转牌圈"
			g.message = ""
			g.mu.Unlock()

			g.runBettingRound("转牌圈")

			g.mu.Lock()
			if g.game.GameOver() {
				g.mu.Unlock()
				g.showdown()
				return
			}
			g.mu.Unlock()

			if !g.shouldSkipBettingRounds() {
				g.mu.Lock()
				g.game.DealRiver()
				g.currentPhase = "河牌圈"
				g.message = ""
				g.mu.Unlock()

				g.runBettingRound("河牌圈")
			}
		}
	}

	g.mu.Lock()
	if len(g.game.CommunityCards) < 3 {
		g.game.DealFlop()
	}
	if len(g.game.CommunityCards) < 4 {
		g.game.DealTurn()
	}
	if len(g.game.CommunityCards) < 5 {
		g.game.DealRiver()
	}
	g.currentPhase = "河牌圈"
	g.mu.Unlock()

	g.showdown()
}

func (g *WebGUIGame) runBettingRound(phase string) {
	g.mu.Lock()

	numPlayers := len(g.game.Players)
	startOffset := 1
	if phase == "翻牌前" {
		startOffset = 3
	}

	startPos := -1
	count := 0
	for i := 0; i < numPlayers*2; i++ {
		pos := (g.game.ButtonPos + 1 + i) % numPlayers
		player := g.game.Players[pos]
		if !player.Folded && !player.AllIn && !player.Bankrupt && player.Chips > 0 {
			count++
			if count == startOffset {
				startPos = pos
				break
			}
		}
	}

	if startPos == -1 {
		g.mu.Unlock()
		return
	}

	activePlayers := g.game.getActivePlayers()
	if len(activePlayers) <= 1 {
		g.mu.Unlock()
		return
	}

	lastRaisePos := -1
	currentPos := startPos
	betCount := 0
	checkCount := 0

	playerActions := make(map[int]Action)

	g.mu.Unlock()

	for {
		g.mu.Lock()
		player := g.game.Players[currentPos]

		if player.Folded || player.AllIn || player.Bankrupt {
			currentPos = (currentPos + 1) % len(g.game.Players)
			g.mu.Unlock()
			continue
		}

		if player.Chips <= 0 {
			player.AllIn = true
			player.Bet = 0
			g.message = fmt.Sprintf("%s 筹码为0，自动全下", player.Name)
			currentPos = (currentPos + 1) % len(g.game.Players)
			g.mu.Unlock()
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if lastRaisePos == currentPos && betCount > 0 {
			g.mu.Unlock()
			break
		}

		g.currentPhase = phase
		g.currentPlayerName = player.Name

		var action Action
		var raiseAmount int

		humanPlayerName := "你"
		if g.currentUser != nil {
			humanPlayerName = g.currentUser.Nickname
		}

		// 判断是否是当前用户操作
		isCurrentUser := player.Name == humanPlayerName

		if isCurrentUser {
			// 当前用户操作
			g.message = "轮到你了！"
			g.waitingForInput = true

			numPlayers := len(g.game.Players)
			dealerPos := g.game.ButtonPos
			bbPos := (dealerPos + 2) % numPlayers

			canCheckAsBB := false
			if phase == "翻牌前" && currentPos == bbPos && lastRaisePos == -1 {
				canCheckAsBB = true
			}

			g.canCheck = g.game.CurrentBet == player.Bet || canCheckAsBB
			g.canCall = g.game.CurrentBet > player.Bet && player.Chips > 0
			g.minRaise = g.game.BigBlind
			if g.game.CurrentBet > 0 {
				g.minRaise = g.game.CurrentBet
			}
			g.maxRaise = player.Chips - (g.game.CurrentBet - player.Bet)
			g.canRaise = player.Chips > (g.game.CurrentBet - player.Bet + g.minRaise)
			g.callAmount = g.game.CurrentBet - player.Bet

			timeoutChan := make(chan bool, 1)
			go func() {
				time.Sleep(60 * time.Second)
				timeoutChan <- true
			}()

			g.mu.Unlock()
			select {
			case actionData := <-g.actionChan:
				action = actionData.Action
				raiseAmount = actionData.Amount
			case <-timeoutChan:
				action = Fold
				g.message = "操作超时，自动弃牌"
			}
			g.mu.Lock()
		} else if g.gameMode == "room" {
			// 房间模式下的其他玩家 - 使用AI自动操作，但显示60秒倒计时
			g.message = fmt.Sprintf("轮到 %s 思考中...", player.Name)
			g.waitingForInput = false
			// 清空操作按钮
			g.canCheck = false
			g.canCall = false
			g.canRaise = false

			// 等待3秒模拟思考时间（或者可以调整为更短）
			g.mu.Unlock()
			time.Sleep(3000 * time.Millisecond)
			g.mu.Lock()
			action, raiseAmount = g.game.getAIAction(player)
		} else {
			// AI模式下的AI玩家
			g.message = fmt.Sprintf("轮到 %s 思考中...", player.Name)
			g.mu.Unlock()
			time.Sleep(3000 * time.Millisecond)
			g.mu.Lock()
			action, raiseAmount = g.game.getAIAction(player)
		}

		playerActions[currentPos] = action

		g.game.executeAction(player, action, raiseAmount)
		g.message = fmt.Sprintf("%s 选择了 %s", player.Name, action)
		g.waitingForInput = false

		if action == Raise || action == AllIn {
			lastRaisePos = currentPos
			betCount++
			checkCount = 0
		} else if action == Check {
			checkCount++
		} else if action == Fold {
			activePlayers = g.game.getActivePlayers()
			if len(activePlayers) <= 1 {
				g.mu.Unlock()
				break
			}
		} else if action == Call {
			checkCount = 0
		}

		activePlayers = g.game.getActivePlayers()
		activeNonAllInCount := 0
		for _, p := range activePlayers {
			if !p.AllIn {
				activeNonAllInCount++
			}
		}

		if activeNonAllInCount <= 1 {
			g.mu.Unlock()
			break
		}

		allChecked := true
		for i, p := range g.game.Players {
			if !p.Folded && !p.AllIn && !p.Bankrupt {
				act, hasActed := playerActions[i]
				if !hasActed || (act != Check && act != Fold) {
					allChecked = false
					break
				}
			}
		}

		numPlayers := len(g.game.Players)
		dealerPos := g.game.ButtonPos
		bbPos := (dealerPos + 2) % numPlayers

		bbChecked := false
		if phase == "翻牌前" && lastRaisePos == -1 {
			if act, hasActed := playerActions[bbPos]; hasActed && (act == Check || act == Fold) {
				bbChecked = true
			}
		}

		if (allChecked || bbChecked) && lastRaisePos == -1 && len(playerActions) >= activeNonAllInCount {
			g.mu.Unlock()
			break
		}

		g.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
		currentPos = (currentPos + 1) % len(g.game.Players)
	}

	g.mu.Lock()
	g.game.resetBets()
	g.message = ""
	g.waitingForInput = false
	g.currentPlayerName = ""
	g.mu.Unlock()
}

func (g *WebGUIGame) showdown() {
	g.mu.Lock()
	g.currentPhase = "摊牌"

	activePlayers := g.game.getActivePlayers()

	var winnerText string
	var bestHand Hand
	var winners []*Player
	var winnerCards []Card

	if len(activePlayers) == 0 {
		// 所有玩家都弃牌，这种情况不应该发生，但做保护
		winnerText = "所有玩家都弃牌，游戏结束"
		g.winnerText = winnerText
		g.gameOver = true
		g.mu.Unlock()
		return
	} else if len(activePlayers) == 1 {
		winner := activePlayers[0]
		winner.Chips += g.game.Pot
		winnerText = fmt.Sprintf("%s 获胜！赢得 %d 筹码！", winner.Name, g.game.Pot)
		winners = []*Player{winner}
		winnerCards = winner.HoleCards
		allCards := append(winner.HoleCards, g.game.CommunityCards...)
		if len(allCards) >= 5 {
			bestHand = EvaluateHand(allCards)
		} else {
			bestHand = Hand{Cards: winner.HoleCards, Rank: HighCard}
		}
	} else {
		type playerHand struct {
			player *Player
			hand   Hand
		}

		var hands []playerHand

		for _, p := range activePlayers {
			allCards := append(p.HoleCards, g.game.CommunityCards...)
			hand := EvaluateHand(allCards)
			hands = append(hands, playerHand{p, hand})
		}

		bestHand = hands[0].hand
		winners = []*Player{hands[0].player}
		winnerCards = hands[0].hand.Cards

		for _, ph := range hands {
			cmp := CompareHands(ph.hand, bestHand)
			if cmp > 0 {
				bestHand = ph.hand
				winners = []*Player{ph.player}
				winnerCards = ph.hand.Cards
			} else if cmp == 0 {
				winners = append(winners, ph.player)
			}
		}

		winAmount := g.game.Pot / len(winners)
		for _, winner := range winners {
			winner.Chips += winAmount
		}

		winnerText = "获胜者: "
		for i, w := range winners {
			if i > 0 {
				winnerText += ", "
			}
			winnerText += w.Name
		}

		if len(winners) > 1 {
			winnerText += fmt.Sprintf(" 每人赢得 %d 筹码！(牌型: %v)", winAmount, bestHand.Rank)
		} else {
			winnerText += fmt.Sprintf(" 赢得 %d 筹码！(牌型: %v)", g.game.Pot, bestHand.Rank)
		}
	}

	sortedCards := make([]Card, len(winnerCards))
	copy(sortedCards, winnerCards)
	for i := 0; i < len(sortedCards); i++ {
		for j := i + 1; j < len(sortedCards); j++ {
			if getRankOrder(sortedCards[i].Rank) < getRankOrder(sortedCards[j].Rank) {
				sortedCards[i], sortedCards[j] = sortedCards[j], sortedCards[i]
			}
		}
	}

	g.showdownCards = make([]string, 0, 5)
	for _, c := range sortedCards {
		g.showdownCards = append(g.showdownCards, c.ImageFileName())
	}

	g.winnerHandRank = getHandRankName(bestHand.Rank)
	g.winnerText = winnerText
	g.gameOver = true
	g.askContinue = false
	g.currentPlayerName = ""
	if len(winners) > 0 {
		g.winnerName = winners[0].Name
		for i := 1; i < len(winners); i++ {
			g.winnerName += "、" + winners[i].Name
		}
	} else {
		g.winnerName = ""
	}

	g.noChips = false
	humanPlayerName := "你"
	if g.currentUser != nil {
		humanPlayerName = g.currentUser.Nickname
	}
	for _, p := range g.game.Players {
		if p.Name == humanPlayerName && p.Chips <= 0 {
			g.noChips = true
			break
		}
	}

	g.game.ButtonPos = (g.game.ButtonPos + 1) % len(g.game.Players)
	g.mu.Unlock()

	time.Sleep(3000 * time.Millisecond)

	g.mu.Lock()
	if !g.noChips {
		g.askContinue = true
	}
	g.mu.Unlock()

	if !g.noChips {
		actionData := <-g.actionChan

		g.mu.Lock()
		if actionData.Amount == -1 {
			g.askContinue = false
			g.askShuffle = true
			g.gameOver = false
			g.mu.Unlock()

			actionData = <-g.actionChan

			g.mu.Lock()
			if actionData.Amount == -2 {
			}
			go g.playHand()
		}
		g.mu.Unlock()
	}
}

func (g *WebGUIGame) getGameState() *GameState {
	state := &GameState{
		Players:         make([]PlayerState, 0, len(g.game.Players)),
		CommunityCards:  make([]string, 0, len(g.game.CommunityCards)),
		Pot:             g.game.Pot,
		Phase:           g.currentPhase,
		CurrentPlayer:   g.currentPlayerName,
		Message:         g.message,
		GameOver:        g.gameOver,
		WinnerText:      g.winnerText,
		CanCheck:        g.canCheck,
		CanCall:         g.canCall,
		CanRaise:        g.canRaise,
		MinRaise:        g.minRaise,
		MaxRaise:        g.maxRaise,
		CallAmount:      g.callAmount,
		WaitingForInput: g.waitingForInput,
		AskContinue:     g.askContinue,
		AskShuffle:      g.askShuffle,
		ShowdownCards:   g.showdownCards,
		WinnerHandRank:  g.winnerHandRank,
		WinnerName:      g.winnerName,
		NoChips:         g.noChips,
		GameMode:        g.gameMode,
	}

	numPlayers := len(g.game.Players)
	dealerPos := g.game.ButtonPos
	sbPos := (dealerPos + 1) % numPlayers
	bbPos := (dealerPos + 2) % numPlayers

	humanPlayerName := "你"
	if g.currentUser != nil {
		humanPlayerName = g.currentUser.Nickname
	}

	for i, p := range g.game.Players {
		if p.Bankrupt {
			continue
		}

		// 在房间模式下，通过玩家名称匹配来判断是否是当前用户
		isHuman := p.Name == humanPlayerName

		ps := PlayerState{
			ID:           p.ID,
			Name:         p.Name,
			Chips:        p.Chips,
			Bet:          p.Bet,
			Folded:       p.Folded,
			AllIn:        p.AllIn,
			IsHuman:      isHuman,
			IsCurrent:    p.Name == g.currentPlayerName,
			IsDealer:     i == dealerPos,
			IsSmallBlind: i == sbPos,
			IsBigBlind:   i == bbPos,
			Cards:        make([]string, 0, 2),
		}

		// 只有当前玩家才能看到自己的牌
		if isHuman {
			for _, c := range p.HoleCards {
				ps.Cards = append(ps.Cards, c.ImageFileName())
			}
		} else if !p.Folded && g.currentPhase == "摊牌" {
			// 摊牌阶段显示所有未弃牌玩家的牌
			for _, c := range p.HoleCards {
				ps.Cards = append(ps.Cards, c.ImageFileName())
			}
		} else {
			// 其他玩家显示背面
			ps.Cards = append(ps.Cards, "bm.png", "bm.png")
		}
		state.Players = append(state.Players, ps)
	}

	for _, c := range g.game.CommunityCards {
		state.CommunityCards = append(state.CommunityCards, c.ImageFileName())
	}

	for len(state.CommunityCards) < 5 {
		state.CommunityCards = append(state.CommunityCards, "bm.png")
	}

	return state
}
