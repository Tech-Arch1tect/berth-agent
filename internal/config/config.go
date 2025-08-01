package config

import (
	"github.com/Tech-Arch1tect/config"
)

type AppConfig struct {
	Port           int    `env:"PORT" validate:"required,max=65535"`
	ComposeDirPath string `env:"COMPOSE_DIR_PATH" validate:"required,min=1"`
	Token          string `env:"TOKEN" validate:"required,min=16"`
	TLSCertFile    string `env:"TLS_CERT_FILE"`
	TLSKeyFile     string `env:"TLS_KEY_FILE"`
}

func (c *AppConfig) SetDefaults() {
	c.Port = 8081
	c.ComposeDirPath = "/opt/compose"
}

func (c *AppConfig) IsHTTPSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

func Load() (*AppConfig, error) {
	var cfg AppConfig
	if err := config.Load(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
