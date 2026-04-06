package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketManager WebSocket连接管理器
type WebSocketManager struct {
	clients    map[int]*Client            // user_id -> client
	rooms      map[string]map[int]*Client // room_id -> map[user_id]client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *BroadcastMessage
	mu         sync.RWMutex
}

// Client WebSocket客户端
type Client struct {
	UserID int
	RoomID string
	Conn   *websocket.Conn
	Send   chan []byte
	mu     sync.Mutex
}

// BroadcastMessage 广播消息
type BroadcastMessage struct {
	RoomID string
	Event  string
	Data   interface{}
}

// WebSocketMessage WebSocket消息格式
type WebSocketMessage struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

var (
	wsManager *WebSocketManager
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许所有来源，生产环境应该限制
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

// NewWebSocketManager 创建WebSocket管理器
func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		clients:    make(map[int]*Client),
		rooms:      make(map[string]map[int]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *BroadcastMessage),
	}
}

// Start 启动WebSocket管理器
func (wm *WebSocketManager) Start() {
	for {
		select {
		case client := <-wm.register:
			wm.mu.Lock()
			wm.clients[client.UserID] = client
			if client.RoomID != "" {
				if wm.rooms[client.RoomID] == nil {
					wm.rooms[client.RoomID] = make(map[int]*Client)
				}
				wm.rooms[client.RoomID][client.UserID] = client
			}
			wm.mu.Unlock()
			fmt.Printf("WebSocket client registered: user_id=%d, room_id=%s\n", client.UserID, client.RoomID)

		case client := <-wm.unregister:
			wm.mu.Lock()
			if _, ok := wm.clients[client.UserID]; ok {
				delete(wm.clients, client.UserID)
				close(client.Send)
			}
			if client.RoomID != "" {
				if room, ok := wm.rooms[client.RoomID]; ok {
					delete(room, client.UserID)
					if len(room) == 0 {
						delete(wm.rooms, client.RoomID)
					}
				}
			}
			wm.mu.Unlock()
			client.Conn.Close()
			fmt.Printf("WebSocket client unregistered: user_id=%d\n", client.UserID)

		case msg := <-wm.broadcast:
			wm.mu.RLock()
			if room, ok := wm.rooms[msg.RoomID]; ok {
				data, _ := json.Marshal(WebSocketMessage{
					Event: msg.Event,
					Data:  msg.Data,
				})
				for _, client := range room {
					select {
					case client.Send <- data:
					default:
						// 客户端发送通道已满，关闭连接
						wm.mu.RUnlock()
						wm.unregister <- client
						wm.mu.RLock()
					}
				}
			}
			wm.mu.RUnlock()
		}
	}
}

// BroadcastToRoom 向房间广播消息
func (wm *WebSocketManager) BroadcastToRoom(roomID, event string, data interface{}) {
	if wm == nil {
		return
	}
	wm.broadcast <- &BroadcastMessage{
		RoomID: roomID,
		Event:  event,
		Data:   data,
	}
}

// SendToUser 向指定用户发送消息
func (wm *WebSocketManager) SendToUser(userID int, event string, data interface{}) {
	if wm == nil {
		return
	}
	wm.mu.RLock()
	client, ok := wm.clients[userID]
	wm.mu.RUnlock()

	if ok {
		msg, _ := json.Marshal(WebSocketMessage{
			Event: event,
			Data:  data,
		})
		select {
		case client.Send <- msg:
		default:
			wm.unregister <- client
		}
	}
}

// ReadPump 读取客户端消息
func (c *Client) ReadPump() {
	defer func() {
		wsManager.unregister <- c
	}()

	c.Conn.SetReadLimit(512 * 1024) // 512KB
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("WebSocket error: %v\n", err)
			}
			break
		}

		// 处理客户端消息
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			c.handleMessage(msg)
		}
	}
}

// WritePump 向客户端写入消息
func (c *Client) WritePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.mu.Lock()
			err := c.Conn.WriteMessage(websocket.TextMessage, message)
			c.mu.Unlock()

			if err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage 处理客户端消息
func (c *Client) handleMessage(msg map[string]interface{}) {
	eventVal, ok := msg["event"]
	if !ok {
		return
	}
	event, _ := eventVal.(string)

	switch event {
	case "join_room":
		// 客户端请求加入房间
		if roomIDVal, ok := msg["room_id"]; ok {
			roomID, _ := roomIDVal.(string)
			if roomID != "" {
				c.RoomID = roomID
				wsManager.mu.Lock()
				if wsManager.rooms[roomID] == nil {
					wsManager.rooms[roomID] = make(map[int]*Client)
				}
				wsManager.rooms[roomID][c.UserID] = c
				wsManager.mu.Unlock()

				// 发送确认消息
				c.Send <- mustJSON(WebSocketMessage{
					Event: "room_joined",
					Data: map[string]string{
						"room_id": roomID,
					},
				})
			}
		}

	case "leave_room":
		// 客户端请求离开房间
		if c.RoomID != "" {
			wsManager.mu.Lock()
			if room, ok := wsManager.rooms[c.RoomID]; ok {
				delete(room, c.UserID)
				if len(room) == 0 {
					delete(wsManager.rooms, c.RoomID)
				}
			}
			c.RoomID = ""
			wsManager.mu.Unlock()
		}

	case "ping":
		// 心跳响应
		c.Send <- mustJSON(WebSocketMessage{
			Event: "pong",
			Data:  map[string]interface{}{"time": time.Now().Unix()},
		})

	case "game_action":
		// 游戏操作
		if c.RoomID == "" {
			return
		}

		game := multiplayerManager.GetGame(c.RoomID)
		if game == nil {
			return
		}

		action, _ := msg["action"].(string)
		amount := 0
		if amt, ok := msg["amount"].(float64); ok {
			amount = int(amt)
		}

		// 提交操作
		err := game.SubmitAction(c.UserID, action, amount)
		if err != nil {
			c.Send <- mustJSON(WebSocketMessage{
				Event: "action_error",
				Data:  map[string]string{"error": err.Error()},
			})
		}

	case "join_game":
		// 加入游戏（从房间等待页面进入游戏）
		if c.RoomID == "" {
			return
		}

		game := multiplayerManager.GetGame(c.RoomID)
		if game != nil {
			game.Join(c.UserID, c)
		}
	}
}

// mustJSON 将数据转为JSON字节
func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// HandleWebSocket 处理WebSocket连接
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 获取当前用户
	user := getCurrentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 升级HTTP连接为WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade error: %v\n", err)
		return
	}

	// 创建客户端
	client := &Client{
		UserID: user.ID,
		Conn:   conn,
		Send:   make(chan []byte, 256),
	}

	// 注册客户端
	wsManager.register <- client

	// 启动读写协程
	go client.WritePump()
	go client.ReadPump()
}

// InitWebSocket 初始化WebSocket
func InitWebSocket() {
	wsManager = NewWebSocketManager()
	go wsManager.Start()
	fmt.Println("WebSocket manager started")
}
