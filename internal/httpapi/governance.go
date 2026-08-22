package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

type GovernanceConfig struct {
	APIKey            string
	DisableAuth       bool
	Timeout           time.Duration
	BodyLimit         string
	RequestsPerSecond float64
	Burst             int
}

func UseGovernance(e *echo.Echo, config GovernanceConfig) {
	e.HTTPErrorHandler = governedErrorHandler
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.BodyLimit == "" {
		config.BodyLimit = "1M"
	}
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = 20
	}
	if config.Burst <= 0 {
		config.Burst = 10
	}

	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.BodyLimit(config.BodyLimit))
	e.Use(requestTimeout(config.Timeout))
	e.Use(accessLog())
	e.Use(ipRateLimit(config.RequestsPerSecond, config.Burst))
	if config.APIKey != "" {
		e.Use(bearerAuth(config.APIKey))
	} else if !config.DisableAuth {
		e.Use(requireConfiguredAuth())
	}
}

func requireConfiguredAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Path() == "/healthz" {
				return next(c)
			}
			return writeError(c, http.StatusServiceUnavailable, "authentication_not_configured", "service authentication is not configured", false)
		}
	}
}

func governedErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status, code, message, retryable := http.StatusInternalServerError, "internal_error", "internal server error", true
	var echoErr *echo.HTTPError
	if errors.As(err, &echoErr) {
		status = echoErr.Code
		switch status {
		case http.StatusRequestEntityTooLarge:
			code, message, retryable = "request_too_large", "request body exceeds limit", false
		case http.StatusNotFound:
			code, message, retryable = "not_found", "resource not found", false
		case http.StatusMethodNotAllowed:
			code, message, retryable = "method_not_allowed", "method not allowed", false
		default:
			code, message, retryable = "http_error", http.StatusText(status), status >= 500
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		status, code, message, retryable = http.StatusGatewayTimeout, "request_timeout", "request timed out", true
	}
	_ = writeError(c, status, code, message, retryable)
}

func bearerAuth(expected string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Path() == "/healthz" {
				return next(c)
			}
			provided := strings.TrimPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
				return writeError(c, http.StatusUnauthorized, "unauthorized", "valid bearer token required", false)
			}
			return next(c)
		}
	}
}

func requestTimeout(timeout time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
			defer cancel()
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

func accessLog() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			started := time.Now()
			err := next(c)
			slog.Info("http request", "request_id", c.Response().Header().Get(echo.HeaderXRequestID), "method", c.Request().Method, "path", c.Path(), "status", c.Response().Status, "duration_ms", time.Since(started).Milliseconds())
			return err
		}
	}
}

func ipRateLimit(requestsPerSecond float64, burst int) echo.MiddlewareFunc {
	type visitor struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}
	var mu sync.Mutex
	visitors := make(map[string]visitor)
	lastCleanup := time.Now()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := c.Request().RemoteAddr
			if host, _, err := net.SplitHostPort(key); err == nil {
				key = host
			}
			mu.Lock()
			now := time.Now()
			if now.Sub(lastCleanup) >= time.Minute {
				for address, value := range visitors {
					if now.Sub(value.lastSeen) > 10*time.Minute {
						delete(visitors, address)
					}
				}
				lastCleanup = now
			}
			value, found := visitors[key]
			if !found {
				value.limiter = rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
			}
			value.lastSeen = now
			visitors[key] = value
			allowed := value.limiter.Allow()
			mu.Unlock()
			if !allowed {
				return writeError(c, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded", true)
			}
			return next(c)
		}
	}
}
