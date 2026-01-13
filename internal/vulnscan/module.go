package vulnscan

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func NewServiceFromConfig(cfg *config.Config, logger *logging.Logger) (*Service, error) {
	svcConfig := ServiceConfig{
		StackLocation:     cfg.StackLocation,
		PersistenceDir:    cfg.VulnscanPersistenceDir,
		PerImageTimeout:   10 * time.Minute,
		TotalTimeout:      30 * time.Minute,
		GrypeScannerURL:   cfg.GrypeScannerURL,
		GrypeScannerToken: cfg.GrypeScannerToken,
	}

	return NewService(svcConfig, logger.With(zap.String("service", "vulnscan")))
}

var Module = fx.Options(
	fx.Provide(NewServiceFromConfig),
	fx.Provide(NewHandler),
)
