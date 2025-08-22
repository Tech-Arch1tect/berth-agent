package config

import (
	"os"

	"go.uber.org/fx"
)

type Config struct {
	AccessToken   string
	Port          string
	StackLocation string
}

func NewConfig() *Config {
	return &Config{
		AccessToken:   getEnv("ACCESS_TOKEN", ""),
		Port:          getEnv("PORT", "8080"),
		StackLocation: getEnv("STACK_LOCATION", "/opt/compose"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

var Module = fx.Options(
	fx.Provide(NewConfig),
)
