package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hotkhwan/affiliate-api/internal/buildinfo"
)

const requestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type RequestIDGenerator func() (string, error)

type Options struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	BodyLimit    int
	Concurrency  int
	RequestID    RequestIDGenerator
}

type Readiness struct {
	ready atomic.Bool
}

func NewReadiness() *Readiness {
	return &Readiness{}
}

func (r *Readiness) Set(value bool) {
	r.ready.Store(value)
}

func New(logger *slog.Logger, metadata buildinfo.Provider, readiness *Readiness, options Options) *fiber.App {
	if options.RequestID == nil {
		options.RequestID = randomRequestID
	}
	app := fiber.New(fiber.Config{
		AppName:               "affiliate-api",
		DisableStartupMessage: true,
		ReadTimeout:           options.ReadTimeout,
		WriteTimeout:          options.WriteTimeout,
		IdleTimeout:           options.IdleTimeout,
		BodyLimit:             options.BodyLimit,
		Concurrency:           options.Concurrency,
	})
	app.Use(requestLogger(logger, options.RequestID))

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

func requestLogger(logger *slog.Logger, generate RequestIDGenerator) fiber.Handler {
	return func(c *fiber.Ctx) error {
		started := time.Now()
		requestID := c.Get(requestIDHeader)
		if !validRequestID.MatchString(requestID) {
			generated, err := generate()
			if err != nil || !validRequestID.MatchString(generated) {
				handlerErr := c.App().ErrorHandler(c, fiber.NewError(fiber.StatusServiceUnavailable, "request ID unavailable"))
				logRequest(logger, c, "", started)
				return handlerErr
			}
			requestID = generated
		}
		c.Set(requestIDHeader, requestID)

		err := c.Next()
		if err != nil {
			err = c.App().ErrorHandler(c, err)
		}
		logRequest(logger, c, requestID, started)
		return err
	}
}

func logRequest(logger *slog.Logger, c *fiber.Ctx, requestID string, started time.Time) {
	logger.Info("request completed",
		"request_id", requestID,
		"method", c.Method(),
		"path", c.Path(),
		"status", c.Response().StatusCode(),
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func randomRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
