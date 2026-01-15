package config

import (
	"os"
	"strconv"

	"go.uber.org/fx"
)

type Config struct {
	AccessToken            string
	Port                   string
	StackLocation          string
	AuditLogEnabled        bool
	AuditLogFilePath       string
	AuditLogSizeLimitMB    int
	LogLevel               string
	VulnscanPersistenceDir string
	GrypeScannerURL        string
	GrypeScannerToken      string
}

func NewConfig() *Config {
	return &Config{
		AccessToken:            getEnv("ACCESS_TOKEN", ""),
		Port:                   getEnv("PORT", "8080"),
		StackLocation:          getEnv("STACK_LOCATION", "/opt/compose"),
		AuditLogEnabled:        getEnvBool("AUDIT_LOG_ENABLED", false),
		AuditLogFilePath:       getEnv("AUDIT_LOG_FILE_PATH", "/var/log/berth-agent/audit.jsonl"),
		AuditLogSizeLimitMB:    getEnvInt("AUDIT_LOG_SIZE_LIMIT_MB", 100),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		VulnscanPersistenceDir: getEnv("VULNSCAN_PERSISTENCE_DIR", "/var/lib/berth-agent/scans"),
		GrypeScannerURL:        getEnv("GRYPE_SCANNER_URL", ""),
		GrypeScannerToken:      getEnv("GRYPE_SCANNER_TOKEN", ""),
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

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

var Module = fx.Options(
	fx.Provide(NewConfig),
)
