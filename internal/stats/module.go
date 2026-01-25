package stats

import (
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewService),
	fx.Provide(NewHandler),
)

func init() {
	_ = logging.Logger{}
}
