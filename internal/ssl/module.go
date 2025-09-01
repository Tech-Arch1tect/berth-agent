package ssl

import "go.uber.org/fx"

var Module = fx.Options(
	fx.Provide(NewCertificateManager),
)
