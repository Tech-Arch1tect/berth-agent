package operations

import (
	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/audit"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceWithConfig),
	fx.Provide(NewHandler),
)

func NewServiceWithConfig(cfg *config.Config, logger *logging.Logger, auditService *audit.Service) *Service {
	return NewService(cfg.StackLocation, cfg.AccessToken, logger, auditService)
}
