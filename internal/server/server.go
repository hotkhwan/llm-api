package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hotkhwan/affiliate-api/internal/buildinfo"
)

const requestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type Readiness struct {
	ready atomic.Bool
}

func NewReadiness() *Readiness {
	return &Readiness{}
}

func (r *Readiness) Set(value bool) {
	r.ready.Store(value)
}

func New(logger *slog.Logger, metadata buildinfo.Provider, readiness *Readiness) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "affiliate-api",
		DisableStartupMessage: true,
	})
	app.Use(requestLogger(logger))

	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/readyz", func(c *fiber.Ctx) error {
		if !readiness.ready.Load() {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "not_ready"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
	app.Get("/version", func(c *fiber.Ctx) error {
		return c.JSON(metadata.Metadata())
	})

	return app
}

func requestLogger(logger *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		started := time.Now()
		requestID := c.Get(requestIDHeader)
		if !validRequestID.MatchString(requestID) {
			requestID = newRequestID()
		}
		c.Set(requestIDHeader, requestID)

		err := c.Next()
		logger.Info("request completed",
			"request_id", requestID,
			"method", c.Method(),
			"path", c.Path(),
			"status", c.Response().StatusCode(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return err
	}
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(bytes[:])
}
