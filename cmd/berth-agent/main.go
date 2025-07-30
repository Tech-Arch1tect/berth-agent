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
	router := server.New(cfg)

	addr := ":" + strconv.Itoa(cfg.Port)

	if cfg.IsHTTPSEnabled() {
		log.Printf("Starting Berth Agent with HTTPS on %s", addr)
		log.Printf("Using TLS cert: %s, key: %s", cfg.TLSCertFile, cfg.TLSKeyFile)
		if err := router.ListenAndServeTLS(addr, cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			log.Fatal("Failed to start HTTPS server:", err)
		}
	} else {
		log.Printf("Starting Berth Agent with HTTP on %s", addr)
		if err := router.ListenAndServe(addr); err != nil {
			log.Fatal("Failed to start HTTP server:", err)
		}
	}
}
