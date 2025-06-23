package config

import (
	"github.com/Tech-Arch1tect/config"
)

type AppConfig struct {
	Port int `env:"PORT" validate:"required,max=65535"`
}

func (c *AppConfig) SetDefaults() {
	c.Port = 8081
}

func Load() (*AppConfig, error) {
	var cfg AppConfig
	if err := config.Load(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}