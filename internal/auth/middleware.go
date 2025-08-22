package auth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func TokenMiddleware(accessToken string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if accessToken == "" {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Access token not configured",
				})
			}

			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Authorization header required",
				})
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Bearer token required",
				})
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token != accessToken {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid token",
				})
			}

			return next(c)
		}
	}
}
