package config

import (
	"os"
	"strconv"

	"go.uber.org/fx"
)

type Config struct {
	AccessToken      string
	Port             string
	StackLocation    string
	AuditLogEnabled  bool
	AuditLogFilePath string
}

func NewConfig() *Config {
	return &Config{
		AccessToken:      getEnv("ACCESS_TOKEN", ""),
		Port:             getEnv("PORT", "8080"),
		StackLocation:    getEnv("STACK_LOCATION", "/opt/compose"),
		AuditLogEnabled:  getEnvBool("AUDIT_LOG_ENABLED", false),
		AuditLogFilePath: getEnv("AUDIT_LOG_FILE_PATH", "/var/log/berth-agent/audit.jsonl"),
	}
}

func (c *Config) GetRequestLogEnabled() bool {
	return c.AuditLogEnabled
}

func (c *Config) GetRequestLogFilePath() string {
	return c.AuditLogFilePath
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

var Module = fx.Options(
	fx.Provide(NewConfig),
)
