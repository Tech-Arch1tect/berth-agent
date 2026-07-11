package backup

import (
	"context"

	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceWithConfig),
	fx.Provide(NewHandler),
	fx.Invoke(RunStartupHygiene),
)

func NewServiceWithConfig(cfg *config.Config, logger *logging.Logger, dockerClient *docker.Client) (*Service, error) {
	return NewService(cfg, logger, dockerClient, docker.NewCommandExecutor(cfg.StackLocation))
}

func RunStartupHygiene(lc fx.Lifecycle, service *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			service.MarkInterruptedRuns()
			service.RemoveOrphanedHelpers(ctx)
			return nil
		},
	})
}
