package main

import (
	"fmt"
	"log"
	"strconv"

	"berth-agent/internal/config"
	"berth-agent/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	fmt.Printf("Loaded configuration: %+v\n", cfg)
	srv := server.New(cfg)

	log.Println("Starting Berth Agent on :" + strconv.Itoa(cfg.Port))
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
