package main

import (
	"log"
	
	"berth-agent/internal/server"
)

func main() {
	srv := server.New()

	log.Println("Starting Berth Agent on :8081")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
