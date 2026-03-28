package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ==================== 邀请码生成 ====================

// GenerateInviteCode 生成6位邀请码
func GenerateInviteCode() string {
	chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	code := make([]byte, 6)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		code[i] = chars[n.Int64()]
	}
	return string(code)
}

// GenerateRoomID 生成可读的房间号
func GenerateRoomID() string {
	// 生成5位数字房间号
	n, _ := rand.Int(rand.Reader, big.NewInt(90000))
	return fmt.Sprintf("%05d", 10000+n.Int64())
}

// ==================== 房间管理器（内存缓存）====================

type RoomManager struct {
	rooms map[string]*RoomCache // room_id -> room cache
	mu    sync.RWMutex
}

type RoomCache struct {
	RoomID    string
	RoomCode  string
	CreatorID int
	Players   map[int]*PlayerCache // user_id -> player
	Status    string
	UpdatedAt time.Time
}

type PlayerCache struct {
	UserID   int
	Nickname string
	IsReady  bool
	IsHost   bool
	SeatIndex int
}

var roomManager = &RoomManager{
	rooms: make(map[string]*RoomCache),
}

func (rm *RoomManager) GetRoom(roomID string) *RoomCache {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.rooms[roomID]
}

func (rm *RoomManager) SetRoom(room *RoomCache) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.rooms[room.RoomID] = room
}

func (rm *RoomManager) DeleteRoom(roomID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.rooms, roomID)
}

// ==================== 房间API处理器 ====================

// CreateRoomRequest 创建房间请求
type CreateRoomRequest struct {
	RoomName   string `json:"room_name"`
	Password   string `json:"password"`
	MaxPlayers int    `json:"max_players"`
	GameConfig string `json:"game_config"`
}

// CreateRoomResponse 创建房间响应
type CreateRoomResponse struct {
	RoomID     string `json:"room_id"`
	RoomCode   string `json:"room_code"`
	InviteURL  string `json:"invite_url"`
	RoomName   string `json:"room_name"`
	MaxPlayers int    `json:"max_players"`
}

// HandleCreateRoom 处理创建房间请求
func HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	// 获取当前用户
	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	// 检查用户是否已在其他房间
	existingRoom, err := GetUserCurrentRoom(user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "检查房间状态失败: " + err.Error(),
		})
		return
	}
	if existingRoom != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "您已在其他房间中，请先离开当前房间",
			"data": map[string]string{
				"room_id": existingRoom.RoomID,
			},
		})
		return
	}

	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 验证参数
	if req.RoomName == "" {
		req.RoomName = user.Nickname + "的房间"
	}
	if req.MaxPlayers < 2 || req.MaxPlayers > 10 {
		req.MaxPlayers = 6
	}

	// 处理 game_config，如果为空则设为 nil
	var gameConfig interface{}
	if req.GameConfig != "" {
		gameConfig = req.GameConfig
	} else {
		gameConfig = nil
	}

	// 生成房间ID和邀请码
	roomID := GenerateRoomID()
	roomCode := GenerateInviteCode()

	// 确保房间码唯一
	for {
		exists, _ := CheckRoomCodeExists(roomCode)
		if !exists {
			break
		}
		roomCode = GenerateInviteCode()
	}

	// 创建房间
	_, err = CreateRoom(roomID, roomCode, user.ID, req.RoomName, req.Password, req.MaxPlayers, gameConfig)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "创建房间失败: " + err.Error(),
		})
		return
	}

	// 添加房主到房间
	err = AddRoomPlayer(roomID, user.ID, 0)
	if err != nil {
		// 回滚房间创建
		DeleteRoom(roomID)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "加入房间失败: " + err.Error(),
		})
		return
	}

	// 更新内存缓存
	roomManager.SetRoom(&RoomCache{
		RoomID:    roomID,
		RoomCode:  roomCode,
		CreatorID: user.ID,
		Players: map[int]*PlayerCache{
			user.ID: {
				UserID:   user.ID,
				Nickname: user.Nickname,
				IsReady:  false,
				IsHost:   true,
				SeatIndex: 0,
			},
		},
		Status:    "waiting",
		UpdatedAt: time.Now(),
	})

	// 生成邀请链接
	inviteURL := fmt.Sprintf("%s://%s/room/join?code=%s", getScheme(r), r.Host, roomCode)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "房间创建成功",
		"data": CreateRoomResponse{
			RoomID:     roomID,
			RoomCode:   roomCode,
			InviteURL:  inviteURL,
			RoomName:   req.RoomName,
			MaxPlayers: req.MaxPlayers,
		},
	})
}

// JoinRoomRequest 加入房间请求
type JoinRoomRequest struct {
	RoomID   string `json:"room_id,omitempty"`
	RoomCode string `json:"room_code,omitempty"`
	Password string `json:"password,omitempty"`
}

// HandleJoinRoom 处理加入房间请求
func HandleJoinRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	// 获取当前用户
	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req JoinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 查找房间
	var room *Room
	var err error

	if req.RoomCode != "" {
		room, err = GetRoomByCode(strings.ToUpper(req.RoomCode))
	} else if req.RoomID != "" {
		room, err = GetRoomByID(req.RoomID)
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "请提供房间号或邀请码",
		})
		return
	}

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "查询房间失败",
		})
		return
	}

	if room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	// 检查房间状态
	if room.Status == "gaming" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间正在游戏中",
		})
		return
	}

	if room.Status == "closed" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间已关闭",
		})
		return
	}

	// 检查是否已在房间中
	existingPlayer, _ := GetRoomPlayer(room.RoomID, user.ID)
	if existingPlayer != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "您已在房间中",
			"data": map[string]string{
				"room_id": room.RoomID,
			},
		})
		return
	}

	// 检查是否已在其他房间
	userRoom, _ := GetUserCurrentRoom(user.ID)
	if userRoom != nil && userRoom.RoomID != room.RoomID {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "您已在其他房间中，请先离开当前房间",
		})
		return
	}

	// 检查房间是否已满
	players, _ := GetRoomPlayers(room.RoomID)
	if len(players) >= room.MaxPlayers {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间已满",
		})
		return
	}

	// 验证密码
	if room.Password != "" && room.Password != req.Password {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间密码错误",
		})
		return
	}

	// 分配座位号
	seatIndex := len(players)

	// 添加玩家到房间
	err = AddRoomPlayer(room.RoomID, user.ID, seatIndex)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "加入房间失败: " + err.Error(),
		})
		return
	}

	// 更新玩家数量
	UpdateRoomPlayerCount(room.RoomID, len(players)+1)

	// 更新内存缓存
	roomCache := roomManager.GetRoom(room.RoomID)
	if roomCache != nil {
		roomCache.Players[user.ID] = &PlayerCache{
			UserID:    user.ID,
			Nickname:  user.Nickname,
			IsReady:   false,
			IsHost:    false,
			SeatIndex: seatIndex,
		}
	}

	// 广播玩家加入消息
	broadcastToRoom(room.RoomID, "room_player_join", map[string]interface{}{
		"user_id":   user.ID,
		"nickname":  user.Nickname,
		"seat_index": seatIndex,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "加入房间成功",
		"data": map[string]string{
			"room_id": room.RoomID,
		},
	})
}

// HandleGetRoom 处理获取房间信息请求
func HandleGetRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	// 获取房间ID或房间码
	roomID := r.URL.Query().Get("room_id")
	roomCode := r.URL.Query().Get("room_code")

	var room *Room
	var err error

	if roomCode != "" {
		room, err = GetRoomByCode(strings.ToUpper(roomCode))
	} else if roomID != "" {
		room, err = GetRoomByID(roomID)
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "请提供房间号或邀请码",
		})
		return
	}

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "查询房间失败",
		})
		return
	}

	if room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	// 获取玩家列表
	players, err := GetRoomPlayers(room.RoomID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取玩家列表失败",
		})
		return
	}

	// 检查当前用户是否在房间中
	user := getCurrentUser(r)
	isInRoom := false
	if user != nil {
		for _, p := range players {
			if p.UserID == user.ID {
				isInRoom = true
				break
			}
		}
	}

	// 构造响应（隐藏密码）
	roomData := map[string]interface{}{
		"room_id":         room.RoomID,
		"room_code":       room.RoomCode,
		"room_name":       room.RoomName,
		"creator_id":      room.CreatorID,
		"max_players":     room.MaxPlayers,
		"current_players": len(players),
		"status":          room.Status,
		"has_password":    room.Password != "",
		"created_at":      room.CreatedAt,
		"players":         players,
		"is_in_room":      isInRoom,
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    roomData,
	})
}

// HandleLeaveRoom 处理离开房间请求
func HandleLeaveRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	// 获取当前用户
	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	// 获取房间ID
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 检查用户是否在房间中
	player, err := GetRoomPlayer(req.RoomID, user.ID)
	if err != nil || player == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "您不在该房间中",
		})
		return
	}

	// 获取房间信息
	room, err := GetRoomByID(req.RoomID)
	if err != nil || room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	// 移除玩家
	err = RemoveRoomPlayer(req.RoomID, user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "离开房间失败",
		})
		return
	}

	// 获取剩余玩家
	remainingPlayers, _ := GetRoomPlayers(req.RoomID)
	newCount := len(remainingPlayers)
	UpdateRoomPlayerCount(req.RoomID, newCount)

	// 如果是房主离开，转让房主
	if room.CreatorID == user.ID && newCount > 0 {
		newHost := remainingPlayers[0]
		TransferRoomHost(req.RoomID, newHost.UserID)
	}

	// 如果房间空了，关闭房间
	if newCount == 0 {
		UpdateRoomStatus(req.RoomID, "closed")
		roomManager.DeleteRoom(req.RoomID)
	} else {
		// 更新内存缓存
		roomCache := roomManager.GetRoom(req.RoomID)
		if roomCache != nil {
			delete(roomCache.Players, user.ID)
		}

		// 广播玩家离开消息
		broadcastToRoom(req.RoomID, "room_player_leave", map[string]interface{}{
			"user_id":  user.ID,
			"nickname": user.Nickname,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "离开房间成功",
	})
}

// HandleSetReady 处理准备/取消准备请求
func HandleSetReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		RoomID  string `json:"room_id"`
		IsReady bool   `json:"is_ready"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 检查玩家是否在房间中
	player, err := GetRoomPlayer(req.RoomID, user.ID)
	if err != nil || player == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "您不在该房间中",
		})
		return
	}

	// 更新准备状态
	err = UpdatePlayerReadyStatus(req.RoomID, user.ID, req.IsReady)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "更新准备状态失败",
		})
		return
	}

	// 更新内存缓存
	roomCache := roomManager.GetRoom(req.RoomID)
	if roomCache != nil {
		if p, ok := roomCache.Players[user.ID]; ok {
			p.IsReady = req.IsReady
		}
	}

	// 广播准备状态变化
	broadcastToRoom(req.RoomID, "room_player_ready", map[string]interface{}{
		"user_id":  user.ID,
		"nickname": user.Nickname,
		"is_ready": req.IsReady,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "准备状态更新成功",
	})
}

// HandleStartGame 处理开始游戏请求（仅房主）
func HandleStartGame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 获取房间信息
	room, err := GetRoomByID(req.RoomID)
	if err != nil || room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	// 检查是否是房主
	if room.CreatorID != user.ID {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只有房主才能开始游戏",
		})
		return
	}

	// 获取玩家列表
	players, err := GetRoomPlayers(req.RoomID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取玩家列表失败",
		})
		return
	}

	// 检查人数
	if len(players) < 2 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "至少需要2人才能开始游戏",
		})
		return
	}

	// 检查是否所有玩家都准备
	for _, p := range players {
		if !p.IsReady && p.UserID != room.CreatorID {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "还有玩家未准备",
			})
			return
		}
	}

	// 更新房间状态
	err = UpdateRoomStatus(req.RoomID, "gaming")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "开始游戏失败",
		})
		return
	}

	// 更新内存缓存
	roomCache := roomManager.GetRoom(req.RoomID)
	if roomCache != nil {
		roomCache.Status = "gaming"
	}

	// 广播游戏开始
	broadcastToRoom(req.RoomID, "room_game_start", map[string]interface{}{
		"room_id": req.RoomID,
		"players": players,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "游戏开始",
	})
}

// HandleKickPlayer 处理踢人请求（仅房主）
func HandleKickPlayer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		RoomID string `json:"room_id"`
		UserID int    `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 获取房间信息
	room, err := GetRoomByID(req.RoomID)
	if err != nil || room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	// 检查是否是房主
	if room.CreatorID != user.ID {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只有房主才能踢人",
		})
		return
	}

	// 不能踢自己
	if req.UserID == user.ID {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "不能踢出自己",
		})
		return
	}

	// 获取被踢玩家信息
	kickedPlayer, _ := GetRoomPlayer(req.RoomID, req.UserID)
	if kickedPlayer == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "玩家不在房间中",
		})
		return
	}

	// 移除玩家
	err = RemoveRoomPlayer(req.RoomID, req.UserID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "踢人失败",
		})
		return
	}

	// 更新玩家数量
	remainingPlayers, _ := GetRoomPlayers(req.RoomID)
	UpdateRoomPlayerCount(req.RoomID, len(remainingPlayers))

	// 更新内存缓存
	roomCache := roomManager.GetRoom(req.RoomID)
	if roomCache != nil {
		delete(roomCache.Players, req.UserID)
	}

	// 广播玩家被踢消息
	broadcastToRoom(req.RoomID, "room_player_kicked", map[string]interface{}{
		"user_id":  req.UserID,
		"nickname": kickedPlayer.Nickname,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "踢人成功",
	})
}

// HandleGetMyRoom 处理获取当前用户所在房间请求
func HandleGetMyRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	room, err := GetUserCurrentRoom(user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "查询房间失败",
		})
		return
	}

	if room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    nil,
		})
		return
	}

	// 获取玩家列表
	players, _ := GetRoomPlayers(room.RoomID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"room_id":         room.RoomID,
			"room_code":       room.RoomCode,
			"room_name":       room.RoomName,
			"creator_id":      room.CreatorID,
			"max_players":     room.MaxPlayers,
			"current_players": len(players),
			"status":          room.Status,
			"is_host":         room.CreatorID == user.ID,
			"players":         players,
		},
	})
}

// ==================== 辅助函数 ====================

// getCurrentUser 从请求中获取当前用户
func getCurrentUser(r *http.Request) *User {
	// 从Cookie获取token
	var tokenString string
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

	if tokenString == "" {
		return nil
	}

	claims, err := ParseAccessToken(tokenString)
	if err != nil {
		return nil
	}

	user, err := GetUserByID(claims.UserID)
	if err != nil {
		return nil
	}

	return user
}

// getScheme 获取请求协议
func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}

// broadcastToRoom 向房间所有玩家广播消息（WebSocket）
func broadcastToRoom(roomID, event string, data interface{}) {
	// 这里将通过WebSocketManager实现
	if wsManager != nil {
		wsManager.BroadcastToRoom(roomID, event, data)
	}
}
