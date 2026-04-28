package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

// User 用户结构
type User struct {
	ID          int    `json:"id"`
	Nickname    string `json:"nickname"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	AvatarURL   string `json:"avatar_url"`
	Signature   string `json:"signature"`
	IsOnline    bool   `json:"is_online"`
	ViewCount   int    `json:"view_count"`
}

func InitDB() error {
	dsn := "root:root@tcp(localhost:3306)/texas_poker?charset=utf8mb4&parseTime=True&loc=Local"

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

func createTables() error {
	createUsersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INT AUTO_INCREMENT PRIMARY KEY,
		nickname VARCHAR(50) NOT NULL UNIQUE,
		email VARCHAR(100) NOT NULL UNIQUE,
		password VARCHAR(255) NOT NULL,
		avatar_url VARCHAR(255) DEFAULT '',
		signature VARCHAR(255) DEFAULT '',
		is_online BOOLEAN DEFAULT FALSE,
		view_count INT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_nickname (nickname),
		INDEX idx_email (email),
		INDEX idx_online (is_online)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err := db.Exec(createUsersTable)
	if err != nil {
		return err
	}

	createRoomsTable := `
	CREATE TABLE IF NOT EXISTS rooms (
		room_id VARCHAR(20) PRIMARY KEY,
		room_code VARCHAR(8) UNIQUE NOT NULL,
		creator_id INT NOT NULL,
		room_name VARCHAR(50) NOT NULL,
		password VARCHAR(20) DEFAULT NULL,
		max_players INT DEFAULT 6,
		current_players INT DEFAULT 1,
		status ENUM('waiting', 'gaming', 'closed') DEFAULT 'waiting',
		game_config JSON DEFAULT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_room_code (room_code),
		INDEX idx_status (status),
		FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createRoomsTable)
	if err != nil {
		return err
	}

	createRoomPlayersTable := `
	CREATE TABLE IF NOT EXISTS room_players (
		id INT AUTO_INCREMENT PRIMARY KEY,
		room_id VARCHAR(20) NOT NULL,
		user_id INT NOT NULL,
		seat_index INT DEFAULT 0,
		is_ready BOOLEAN DEFAULT FALSE,
		joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_room_user (room_id, user_id),
		INDEX idx_room_id (room_id),
		INDEX idx_user_id (user_id),
		FOREIGN KEY (room_id) REFERENCES rooms(room_id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createRoomPlayersTable)
	if err != nil {
		return err
	}

	// 创建Refresh Token表
	createRefreshTokensTable := `
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id INT AUTO_INCREMENT PRIMARY KEY,
		user_id INT NOT NULL,
		token VARCHAR(255) NOT NULL UNIQUE,
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		is_revoked BOOLEAN DEFAULT FALSE,
		INDEX idx_token (token),
		INDEX idx_user_id (user_id),
		INDEX idx_expires_at (expires_at),
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createRefreshTokensTable)
	if err != nil {
		return err
	}

	// 创建好友请求表
	createFriendRequestsTable := `
	CREATE TABLE IF NOT EXISTS friend_requests (
		id INT AUTO_INCREMENT PRIMARY KEY,
		from_user_id INT NOT NULL,
		to_user_id INT NOT NULL,
		status ENUM('pending', 'accepted', 'rejected') DEFAULT 'pending',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY unique_request (from_user_id, to_user_id, status),
		INDEX idx_from_user (from_user_id),
		INDEX idx_to_user (to_user_id),
		INDEX idx_status (status),
		FOREIGN KEY (from_user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (to_user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createFriendRequestsTable)
	if err != nil {
		return err
	}

	// 创建好友关系表
	createFriendsTable := `
	CREATE TABLE IF NOT EXISTS friends (
		id INT AUTO_INCREMENT PRIMARY KEY,
		user_id_1 INT NOT NULL,
		user_id_2 INT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_friendship (user_id_1, user_id_2),
		INDEX idx_user_1 (user_id_1),
		INDEX idx_user_2 (user_id_2),
		FOREIGN KEY (user_id_1) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id_2) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createFriendsTable)
	if err != nil {
		return err
	}

	// 创建私聊消息表
	createMessagesTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id INT AUTO_INCREMENT PRIMARY KEY,
		from_user_id INT NOT NULL,
		to_user_id INT NOT NULL,
		content TEXT NOT NULL,
		is_read BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_from_user (from_user_id),
		INDEX idx_to_user (to_user_id),
		INDEX idx_conversation (from_user_id, to_user_id),
		FOREIGN KEY (from_user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (to_user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createMessagesTable)
	if err != nil {
		return err
	}

	// 创建游戏战绩表
	createGameHistoryTable := `
	CREATE TABLE IF NOT EXISTS game_history (
		id INT AUTO_INCREMENT PRIMARY KEY,
		user_id INT NOT NULL,
		room_id VARCHAR(20) NOT NULL,
		opponent_ids TEXT,
		result ENUM('win', 'lose', 'draw') NOT NULL,
		score INT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_user (user_id),
		INDEX idx_room (room_id),
		INDEX idx_created_at (created_at),
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`

	_, err = db.Exec(createGameHistoryTable)
	if err != nil {
		return err
	}

	return nil
}

func CreateUser(nickname, email, password string) (*User, error) {
	result, err := db.Exec(
		"INSERT INTO users (nickname, email, password) VALUES (?, ?, ?)",
		nickname, email, password,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &User{
		ID:       int(id),
		Nickname: nickname,
		Email:    email,
		Password: password,
	}, nil
}

func GetUserByNickname(nickname string) (*User, error) {
	var user User
	err := db.QueryRow(
		"SELECT id, nickname, email, password, avatar_url, signature, is_online, view_count FROM users WHERE nickname = ?",
		nickname,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func GetUserByEmail(email string) (*User, error) {
	var user User
	err := db.QueryRow(
		"SELECT id, nickname, email, password, avatar_url, signature, is_online, view_count FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func GetUserByID(id int) (*User, error) {
	var user User
	err := db.QueryRow(
		"SELECT id, nickname, email, password, avatar_url, signature, is_online, view_count FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// SearchUsers 搜索用户（基于昵称）
func SearchUsers(nickname string, limit int) ([]*User, error) {
	rows, err := db.Query(
		"SELECT id, nickname, email, password, avatar_url, signature, is_online, view_count FROM users WHERE nickname LIKE ? LIMIT ?",
		"%"+nickname+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		err := rows.Scan(&user.ID, &user.Nickname, &user.Email, &user.Password, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	return users, nil
}

// UpdateUserOnlineStatus 更新用户在线状态
func UpdateUserOnlineStatus(userID int, isOnline bool) error {
	_, err := db.Exec("UPDATE users SET is_online = ? WHERE id = ?", isOnline, userID)
	return err
}

// IncrementViewCount 增加用户主页访问次数
func IncrementViewCount(userID int) error {
	_, err := db.Exec("UPDATE users SET view_count = view_count + 1 WHERE id = ?", userID)
	return err
}

// ==================== 好友请求相关结构和函数 ====================

// FriendRequest 好友请求结构
type FriendRequest struct {
	ID         int    `json:"id"`
	FromUserID int    `json:"from_user_id"`
	ToUserID   int    `json:"to_user_id"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

// FriendRequestWithUser 带用户信息的好友请求
type FriendRequestWithUser struct {
	FriendRequest
	FromUser *User `json:"from_user"`
}

// CreateFriendRequest 创建好友请求
func CreateFriendRequest(fromUserID, toUserID int) error {
	_, err := db.Exec(
		"INSERT INTO friend_requests (from_user_id, to_user_id) VALUES (?, ?)",
		fromUserID, toUserID,
	)
	return err
}

// GetFriendRequest 获取好友请求
func GetFriendRequest(fromUserID, toUserID int) (*FriendRequest, error) {
	var req FriendRequest
	err := db.QueryRow(
		"SELECT id, from_user_id, to_user_id, status, created_at FROM friend_requests WHERE from_user_id = ? AND to_user_id = ? AND status = 'pending'",
		fromUserID, toUserID,
	).Scan(&req.ID, &req.FromUserID, &req.ToUserID, &req.Status, &req.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &req, err
}

// AcceptFriendRequest 接受好友请求
func AcceptFriendRequest(requestID int, toUserID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 更新好友请求状态
	_, err = tx.Exec("UPDATE friend_requests SET status = 'accepted' WHERE id = ? AND to_user_id = ?", requestID, toUserID)
	if err != nil {
		return err
	}

	// 获取请求信息
	var fromUserID int
	err = tx.QueryRow("SELECT from_user_id FROM friend_requests WHERE id = ?", requestID).Scan(&fromUserID)
	if err != nil {
		return err
	}

	// 确保user_id_1 < user_id_2，避免重复
	user1 := min(fromUserID, toUserID)
	user2 := max(fromUserID, toUserID)

	// 添加好友关系
	_, err = tx.Exec("INSERT IGNORE INTO friends (user_id_1, user_id_2) VALUES (?, ?)", user1, user2)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// RejectFriendRequest 拒绝好友请求
func RejectFriendRequest(requestID int, toUserID int) error {
	_, err := db.Exec("UPDATE friend_requests SET status = 'rejected' WHERE id = ? AND to_user_id = ?", requestID, toUserID)
	return err
}

// GetIncomingFriendRequests 获取收到的好友请求
func GetIncomingFriendRequests(userID int) ([]*FriendRequestWithUser, error) {
	rows, err := db.Query(`
		SELECT fr.id, fr.from_user_id, fr.to_user_id, fr.status, fr.created_at,
			   u.id, u.nickname, u.avatar_url, u.signature, u.is_online, u.view_count
		FROM friend_requests fr
		JOIN users u ON fr.from_user_id = u.id
		WHERE fr.to_user_id = ? AND fr.status = 'pending'
		ORDER BY fr.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []*FriendRequestWithUser
	for rows.Next() {
		var req FriendRequest
		var user User
		err := rows.Scan(
			&req.ID, &req.FromUserID, &req.ToUserID, &req.Status, &req.CreatedAt,
			&user.ID, &user.Nickname, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount,
		)
		if err != nil {
			return nil, err
		}
		requests = append(requests, &FriendRequestWithUser{
			FriendRequest: req,
			FromUser:      &user,
		})
	}
	return requests, nil
}

// ==================== 好友关系相关结构和函数 ====================

// FriendWithStatus 带状态的好友信息
type FriendWithStatus struct {
	User      *User  `json:"user"`
	IsOnline  bool   `json:"is_online"`
	IsInRoom  bool   `json:"is_in_room"`
	RoomID    string `json:"room_id,omitempty"`
}

// GetFriends 获取好友列表
func GetFriends(userID int) ([]*FriendWithStatus, error) {
	rows, err := db.Query(`
		SELECT u.id, u.nickname, u.email, u.password, u.avatar_url, u.signature, u.is_online, u.view_count
		FROM friends f
		JOIN users u ON (f.user_id_1 = u.id OR f.user_id_2 = u.id)
		WHERE (f.user_id_1 = ? OR f.user_id_2 = ?) AND u.id != ?
	`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []*FriendWithStatus
	for rows.Next() {
		var user User
		err := rows.Scan(&user.ID, &user.Nickname, &user.Email, &user.Password, &user.AvatarURL, &user.Signature, &user.IsOnline, &user.ViewCount)
		if err != nil {
			return nil, err
		}
		friends = append(friends, &FriendWithStatus{
			User:     &user,
			IsOnline: user.IsOnline,
			IsInRoom: false,
		})
	}

	return friends, nil
}

// IsFriend 检查是否是好友
func IsFriend(userID1, userID2 int) (bool, error) {
	user1 := min(userID1, userID2)
	user2 := max(userID1, userID2)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM friends WHERE user_id_1 = ? AND user_id_2 = ?", user1, user2).Scan(&count)
	return count > 0, err
}

// ==================== 消息相关结构和函数 ====================

// Message 消息结构
type Message struct {
	ID         int    `json:"id"`
	FromUserID int    `json:"from_user_id"`
	ToUserID   int    `json:"to_user_id"`
	Content    string `json:"content"`
	IsRead     bool   `json:"is_read"`
	CreatedAt  string `json:"created_at"`
}

// SaveMessage 保存消息
func SaveMessage(fromUserID, toUserID int, content string) (*Message, error) {
	result, err := db.Exec(
		"INSERT INTO messages (from_user_id, to_user_id, content) VALUES (?, ?, ?)",
		fromUserID, toUserID, content,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &Message{
		ID:         int(id),
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Content:    content,
		IsRead:     false,
	}, nil
}

// GetConversation 获取对话历史
func GetConversation(userID1, userID2 int, limit int) ([]*Message, error) {
	rows, err := db.Query(`
		SELECT id, from_user_id, to_user_id, content, is_read, created_at
		FROM messages
		WHERE (from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)
		ORDER BY created_at DESC
		LIMIT ?
	`, userID1, userID2, userID2, userID1, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(&msg.ID, &msg.FromUserID, &msg.ToUserID, &msg.Content, &msg.IsRead, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}

	// 反转消息顺序，使最早的消息在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// MarkMessagesAsRead 标记消息为已读
func MarkMessagesAsRead(fromUserID, toUserID int) error {
	_, err := db.Exec("UPDATE messages SET is_read = TRUE WHERE from_user_id = ? AND to_user_id = ?", fromUserID, toUserID)
	return err
}

// ==================== 游戏战绩相关结构和函数 ====================

// GameRecord 游戏记录结构
type GameRecord struct {
	ID          int    `json:"id"`
	UserID      int    `json:"user_id"`
	RoomID      string `json:"room_id"`
	OpponentIDs string `json:"opponent_ids"`
	Result      string `json:"result"`
	Score       int    `json:"score"`
	CreatedAt   string `json:"created_at"`
}

// GetGameHistory 获取用户游戏历史
func GetGameHistory(userID int, limit int) ([]*GameRecord, error) {
	rows, err := db.Query(`
		SELECT id, user_id, room_id, opponent_ids, result, score, created_at
		FROM game_history
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*GameRecord
	for rows.Next() {
		var rec GameRecord
		err := rows.Scan(&rec.ID, &rec.UserID, &rec.RoomID, &rec.OpponentIDs, &rec.Result, &rec.Score, &rec.CreatedAt)
		if err != nil {
			return nil, err
		}
		records = append(records, &rec)
	}

	return records, nil
}

// AddGameRecord 添加游戏记录
func AddGameRecord(userID int, roomID, opponentIDs, result string, score int) error {
	_, err := db.Exec(
		"INSERT INTO game_history (user_id, room_id, opponent_ids, result, score) VALUES (?, ?, ?, ?, ?)",
		userID, roomID, opponentIDs, result, score,
	)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func CheckNicknameExists(nickname string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE nickname = ?",
		nickname,
	).Scan(&count)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func CheckEmailExists(email string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE email = ?",
		email,
	).Scan(&count)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// ==================== Refresh Token 数据库操作 ====================

// SaveRefreshToken 保存Refresh Token到数据库
func SaveRefreshToken(userID int, token string, expiresAt time.Time) error {
	_, err := db.Exec(
		"INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES (?, ?, ?)",
		userID, token, expiresAt,
	)
	return err
}

// GetRefreshToken 从数据库获取Refresh Token
func GetRefreshToken(token string) (userID int, expiresAt time.Time, isRevoked bool, err error) {
	err = db.QueryRow(
		"SELECT user_id, expires_at, is_revoked FROM refresh_tokens WHERE token = ?",
		token,
	).Scan(&userID, &expiresAt, &isRevoked)
	return
}

// RevokeRefreshToken 撤销Refresh Token
func RevokeRefreshToken(token string) error {
	_, err := db.Exec(
		"UPDATE refresh_tokens SET is_revoked = TRUE WHERE token = ?",
		token,
	)
	return err
}

// RevokeAllUserRefreshTokens 撤销用户的所有Refresh Token
func RevokeAllUserRefreshTokens(userID int) error {
	_, err := db.Exec(
		"UPDATE refresh_tokens SET is_revoked = TRUE WHERE user_id = ?",
		userID,
	)
	return err
}

// CleanExpiredRefreshTokens 清理过期的Refresh Token
func CleanExpiredRefreshTokens() error {
	_, err := db.Exec(
		"DELETE FROM refresh_tokens WHERE expires_at < NOW() OR is_revoked = TRUE",
	)
	return err
}

// ==================== 房间数据库操作 ====================

// Room 房间结构
type Room struct {
	RoomID         string         `json:"room_id"`
	RoomCode       string         `json:"room_code"`
	CreatorID      int            `json:"creator_id"`
	RoomName       string         `json:"room_name"`
	Password       string         `json:"password,omitempty"`
	MaxPlayers     int            `json:"max_players"`
	CurrentPlayers int            `json:"current_players"`
	Status         string         `json:"status"`
	GameConfig     sql.NullString `json:"game_config,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// RoomPlayer 房间玩家结构
type RoomPlayer struct {
	ID        int       `json:"id"`
	RoomID    string    `json:"room_id"`
	UserID    int       `json:"user_id"`
	Nickname  string    `json:"nickname"`
	SeatIndex int       `json:"seat_index"`
	IsReady   bool      `json:"is_ready"`
	JoinedAt  time.Time `json:"joined_at"`
}

// CreateRoom 创建房间
func CreateRoom(roomID, roomCode string, creatorID int, roomName, password string, maxPlayers int, gameConfig interface{}) (*Room, error) {
	_, err := db.Exec(
		"INSERT INTO rooms (room_id, room_code, creator_id, room_name, password, max_players, game_config) VALUES (?, ?, ?, ?, ?, ?, ?)",
		roomID, roomCode, creatorID, roomName, password, maxPlayers, gameConfig,
	)
	if err != nil {
		return nil, err
	}

	var configNullString sql.NullString
	if gameConfig != nil {
		if str, ok := gameConfig.(string); ok && str != "" {
			configNullString = sql.NullString{String: str, Valid: true}
		}
	}

	return &Room{
		RoomID:     roomID,
		RoomCode:   roomCode,
		CreatorID:  creatorID,
		RoomName:   roomName,
		Password:   password,
		MaxPlayers: maxPlayers,
		Status:     "waiting",
		GameConfig: configNullString,
	}, nil
}

// GetRoomByID 通过房间ID获取房间
func GetRoomByID(roomID string) (*Room, error) {
	var room Room
	err := db.QueryRow(
		"SELECT room_id, room_code, creator_id, room_name, password, max_players, current_players, status, game_config, created_at FROM rooms WHERE room_id = ?",
		roomID,
	).Scan(&room.RoomID, &room.RoomCode, &room.CreatorID, &room.RoomName, &room.Password, &room.MaxPlayers, &room.CurrentPlayers, &room.Status, &room.GameConfig, &room.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &room, nil
}

// GetRoomByCode 通过房间码获取房间
func GetRoomByCode(roomCode string) (*Room, error) {
	var room Room
	err := db.QueryRow(
		"SELECT room_id, room_code, creator_id, room_name, password, max_players, current_players, status, game_config, created_at FROM rooms WHERE room_code = ?",
		roomCode,
	).Scan(&room.RoomID, &room.RoomCode, &room.CreatorID, &room.RoomName, &room.Password, &room.MaxPlayers, &room.CurrentPlayers, &room.Status, &room.GameConfig, &room.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &room, nil
}

// UpdateRoomStatus 更新房间状态
func UpdateRoomStatus(roomID, status string) error {
	_, err := db.Exec("UPDATE rooms SET status = ? WHERE room_id = ?", status, roomID)
	return err
}

// UpdateRoomPlayerCount 更新房间玩家数量
func UpdateRoomPlayerCount(roomID string, count int) error {
	_, err := db.Exec("UPDATE rooms SET current_players = ? WHERE room_id = ?", count, roomID)
	return err
}

// DeleteRoom 删除房间
func DeleteRoom(roomID string) error {
	_, err := db.Exec("DELETE FROM rooms WHERE room_id = ?", roomID)
	return err
}

// AddRoomPlayer 添加玩家到房间
func AddRoomPlayer(roomID string, userID int, seatIndex int) error {
	_, err := db.Exec(
		"INSERT INTO room_players (room_id, user_id, seat_index) VALUES (?, ?, ?)",
		roomID, userID, seatIndex,
	)
	return err
}

// RemoveRoomPlayer 从房间移除玩家
func RemoveRoomPlayer(roomID string, userID int) error {
	_, err := db.Exec("DELETE FROM room_players WHERE room_id = ? AND user_id = ?", roomID, userID)
	return err
}

// GetRoomPlayers 获取房间所有玩家
func GetRoomPlayers(roomID string) ([]*RoomPlayer, error) {
	rows, err := db.Query(
		`SELECT rp.id, rp.room_id, rp.user_id, u.nickname, rp.seat_index, rp.is_ready, rp.joined_at 
		 FROM room_players rp 
		 JOIN users u ON rp.user_id = u.id 
		 WHERE rp.room_id = ? 
		 ORDER BY rp.seat_index`,
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []*RoomPlayer
	for rows.Next() {
		var p RoomPlayer
		err := rows.Scan(&p.ID, &p.RoomID, &p.UserID, &p.Nickname, &p.SeatIndex, &p.IsReady, &p.JoinedAt)
		if err != nil {
			return nil, err
		}
		players = append(players, &p)
	}

	return players, nil
}

// GetRoomPlayer 获取房间中的特定玩家
func GetRoomPlayer(roomID string, userID int) (*RoomPlayer, error) {
	var p RoomPlayer
	err := db.QueryRow(
		`SELECT rp.id, rp.room_id, rp.user_id, u.nickname, rp.seat_index, rp.is_ready, rp.joined_at 
		 FROM room_players rp 
		 JOIN users u ON rp.user_id = u.id 
		 WHERE rp.room_id = ? AND rp.user_id = ?`,
		roomID, userID,
	).Scan(&p.ID, &p.RoomID, &p.UserID, &p.Nickname, &p.SeatIndex, &p.IsReady, &p.JoinedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// UpdatePlayerReadyStatus 更新玩家准备状态
func UpdatePlayerReadyStatus(roomID string, userID int, isReady bool) error {
	_, err := db.Exec("UPDATE room_players SET is_ready = ? WHERE room_id = ? AND user_id = ?", isReady, roomID, userID)
	return err
}

// GetUserCurrentRoom 获取用户当前所在的房间
func GetUserCurrentRoom(userID int) (*Room, error) {
	var roomID string
	err := db.QueryRow("SELECT room_id FROM room_players WHERE user_id = ? LIMIT 1", userID).Scan(&roomID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return GetRoomByID(roomID)
}

// CheckRoomCodeExists 检查房间码是否已存在
func CheckRoomCodeExists(roomCode string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM rooms WHERE room_code = ?", roomCode).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TransferRoomHost 转让房主
func TransferRoomHost(roomID string, newHostID int) error {
	_, err := db.Exec("UPDATE rooms SET creator_id = ? WHERE room_id = ?", newHostID, roomID)
	return err
}

// ResetNonHostReadyStatus 重置非房主玩家的准备状态
func ResetNonHostReadyStatus(roomID string, creatorID int) error {
	_, err := db.Exec("UPDATE room_players SET is_ready = FALSE WHERE room_id = ? AND user_id != ?", roomID, creatorID)
	return err
}

// UpdateRoomStatusToWaiting 将房间状态更新为等待中
func UpdateRoomStatusToWaiting(roomID string) error {
	_, err := db.Exec("UPDATE rooms SET status = 'waiting' WHERE room_id = ?", roomID)
	return err
}
