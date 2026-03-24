package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

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
