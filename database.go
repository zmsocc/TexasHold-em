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
	ID       int    `json:"id"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Password string `json:"password"`
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
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_nickname (nickname),
		INDEX idx_email (email)
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
		"SELECT id, nickname, email, password FROM users WHERE nickname = ?",
		nickname,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password)

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
		"SELECT id, nickname, email, password FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password)

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
		"SELECT id, nickname, email, password FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Nickname, &user.Email, &user.Password)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
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
