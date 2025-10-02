package logging

import (
	"berth-agent/config"
	"context"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceFromConfig),
	fx.Invoke(RegisterShutdown),
)

func NewServiceFromConfig(cfg *config.Config) (*Service, error) {
	return NewService(
		cfg.AuditLogEnabled,
		cfg.AuditLogFilePath,
	)
}

func RegisterShutdown(lc fx.Lifecycle, service *Service) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return service.Close()
		},
	})
}
