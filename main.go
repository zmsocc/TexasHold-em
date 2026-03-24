package main

import (
	"log"
)

func main() {
	log.Println("Initializing database...")
	if err := InitDB(); err != nil {
		log.Printf("Warning: Failed to initialize MySQL database: %v", err)
		log.Println("Falling back to in-memory storage")
	} else {
		log.Println("MySQL database initialized successfully")
	}

	guiGame := NewWebGUIGame()
	guiGame.Run()
}
