package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// RoomStatus 房间状态
type RoomStatus int

const (
	RoomStatusWaiting RoomStatus = iota // 等待中
	RoomStatusPlaying                   // 游戏中
	RoomStatusClosed                    // 已关闭
)

// RoomPlayer 房间中的玩家
type RoomPlayer struct {
	UserID   int    `json:"userId"`
	Nickname string `json:"nickname"`
	IsReady  bool   `json:"isReady"`
	IsHost   bool   `json:"isHost"`
}

// Room 游戏房间
type Room struct {
	ID        string         `json:"id"`
	Code      string         `json:"code"`      // 房间码，用于搜索
	Name      string         `json:"name"`      // 房间名称
	HostID    int            `json:"hostId"`    // 房主ID
	Players   []*RoomPlayer  `json:"players"`   // 房间中的玩家
	Status    RoomStatus     `json:"status"`    // 房间状态
	MaxPlayers int           `json:"maxPlayers"` // 最大玩家数
	CreatedAt time.Time      `json:"createdAt"`
	mu        sync.RWMutex
}

// RoomManager 房间管理器
type RoomManager struct {
	rooms map[string]*Room // key: room ID
	codes map[string]string // key: room code, value: room ID
	mu    sync.RWMutex
}

// NewRoomManager 创建房间管理器
func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Room),
		codes: make(map[string]string),
	}
}

// generateRoomCode 生成6位房间码
func generateRoomCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	code := make([]byte, 6)
	for i := range code {
		code[i] = chars[rand.Intn(len(chars))]
	}
	return string(code)
}

// CreateRoom 创建房间
func (rm *RoomManager) CreateRoom(hostID int, hostName string, roomName string, maxPlayers int) (*Room, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if maxPlayers < 2 || maxPlayers > 10 {
		return nil, fmt.Errorf("房间人数必须在2-10之间")
	}

	// 生成唯一房间ID和房间码
	roomID := fmt.Sprintf("room_%d_%d", hostID, time.Now().UnixNano())
	roomCode := generateRoomCode()
	
	// 确保房间码唯一
	for rm.codes[roomCode] != "" {
		roomCode = generateRoomCode()
	}

	room := &Room{
		ID:         roomID,
		Code:       roomCode,
		Name:       roomName,
		HostID:     hostID,
		Players:    []*RoomPlayer{},
		Status:     RoomStatusWaiting,
		MaxPlayers: maxPlayers,
		CreatedAt:  time.Now(),
	}

	// 添加房主
	host := &RoomPlayer{
		UserID:   hostID,
		Nickname: hostName,
		IsReady:  false,
		IsHost:   true,
	}
	room.Players = append(room.Players, host)

	rm.rooms[roomID] = room
	rm.codes[roomCode] = roomID

	return room, nil
}

// GetRoomByID 通过ID获取房间
func (rm *RoomManager) GetRoomByID(roomID string) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.rooms[roomID]
}

// GetRoomByCode 通过房间码获取房间
func (rm *RoomManager) GetRoomByCode(code string) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	
	roomID := rm.codes[code]
	if roomID == "" {
		return nil
	}
	return rm.rooms[roomID]
}

// JoinRoom 加入房间
func (rm *RoomManager) JoinRoom(roomID string, userID int, nickname string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	room := rm.rooms[roomID]
	if room == nil {
		return fmt.Errorf("房间不存在")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if room.Status != RoomStatusWaiting {
		return fmt.Errorf("房间已开始游戏")
	}

	if len(room.Players) >= room.MaxPlayers {
		return fmt.Errorf("房间已满")
	}

	// 检查是否已在房间中
	for _, p := range room.Players {
		if p.UserID == userID {
			return fmt.Errorf("您已在房间中")
		}
	}

	player := &RoomPlayer{
		UserID:   userID,
		Nickname: nickname,
		IsReady:  false,
		IsHost:   false,
	}
	room.Players = append(room.Players, player)

	return nil
}

// LeaveRoom 离开房间
func (rm *RoomManager) LeaveRoom(roomID string, userID int) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	room := rm.rooms[roomID]
	if room == nil {
		return fmt.Errorf("房间不存在")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	// 找到玩家并移除
	for i, p := range room.Players {
		if p.UserID == userID {
			room.Players = append(room.Players[:i], room.Players[i+1:]...)
			
			// 如果房主离开，转让房主
			if p.IsHost && len(room.Players) > 0 {
				room.Players[0].IsHost = true
				room.HostID = room.Players[0].UserID
			}
			
			// 如果房间空了，关闭房间
			if len(room.Players) == 0 {
				room.Status = RoomStatusClosed
				delete(rm.codes, room.Code)
				delete(rm.rooms, roomID)
			}
			
			return nil
		}
	}

	return fmt.Errorf("玩家不在房间中")
}

// SetPlayerReady 设置玩家准备状态
func (rm *RoomManager) SetPlayerReady(roomID string, userID int, ready bool) error {
	room := rm.GetRoomByID(roomID)
	if room == nil {
		return fmt.Errorf("房间不存在")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	for _, p := range room.Players {
		if p.UserID == userID {
			p.IsReady = ready
			return nil
		}
	}

	return fmt.Errorf("玩家不在房间中")
}

// CanStartGame 检查是否可以开始游戏
func (rm *RoomManager) CanStartGame(roomID string) (bool, error) {
	room := rm.GetRoomByID(roomID)
	if room == nil {
		return false, fmt.Errorf("房间不存在")
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	if len(room.Players) < 2 {
		return false, fmt.Errorf("至少需要2人才能开始游戏")
	}

	// 检查是否所有玩家都准备
	for _, p := range room.Players {
		if !p.IsReady && !p.IsHost {
			return false, fmt.Errorf("还有玩家未准备")
		}
	}

	return true, nil
}

// StartGame 开始游戏
func (rm *RoomManager) StartGame(roomID string) error {
	room := rm.GetRoomByID(roomID)
	if room == nil {
		return fmt.Errorf("房间不存在")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if room.Status != RoomStatusWaiting {
		return fmt.Errorf("房间已开始游戏")
	}

	room.Status = RoomStatusPlaying
	return nil
}

// GetRoomList 获取房间列表
func (rm *RoomManager) GetRoomList() []*Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var list []*Room
	for _, room := range rm.rooms {
		if room.Status == RoomStatusWaiting {
			list = append(list, room)
		}
	}
	return list
}

// GetPlayerRoom 获取玩家所在的房间
func (rm *RoomManager) GetPlayerRoom(userID int) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		room.mu.RLock()
		for _, p := range room.Players {
			if p.UserID == userID {
				room.mu.RUnlock()
				return room
			}
		}
		room.mu.RUnlock()
	}
	return nil
}
