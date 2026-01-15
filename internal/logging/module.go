package logging

import (
	"berth-agent/config"
	"context"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceFromConfig),
	fx.Provide(NewLoggerFromConfig),
	fx.Invoke(RegisterShutdown),
	fx.Invoke(RegisterLoggerShutdown),
)

func NewServiceFromConfig(cfg *config.Config) (*Service, error) {
	maxSizeBytes := int64(cfg.AuditLogSizeLimitMB) * 1024 * 1024
	return NewService(
		cfg.AuditLogEnabled,
		cfg.AuditLogFilePath,
		maxSizeBytes,
	)
}

func NewLoggerFromConfig(cfg *config.Config) (*Logger, error) {
	return NewLogger(cfg.LogLevel)
}

func RegisterShutdown(lc fx.Lifecycle, service *Service) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return service.Close()
		},
	})
}

func RegisterLoggerShutdown(lc fx.Lifecycle, logger *Logger) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return logger.Sync()
		},
	})
}
