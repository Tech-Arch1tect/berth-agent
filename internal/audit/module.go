package audit

import (
	"context"
	"github.com/tech-arch1tect/berth-agent/config"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceFromConfig),
	fx.Invoke(RegisterShutdown),
)

func NewServiceFromConfig(cfg *config.Config) (*Service, error) {
	maxSizeBytes := int64(cfg.AuditLogSizeLimitMB) * 1024 * 1024
	return NewService(
		cfg.AuditLogEnabled,
		cfg.AuditLogFilePath,
		maxSizeBytes,
	)
}

func RegisterShutdown(lc fx.Lifecycle, service *Service) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return service.Close()
		},
	})
}
