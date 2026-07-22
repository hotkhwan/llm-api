package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type Config struct {
	Address         string
	ShutdownTimeout time.Duration
}

type Service interface {
	Listener(net.Listener) error
	ShutdownWithTimeout(time.Duration) error
}

type Readiness interface {
	Set(bool)
}

type ListenFunc func(network, address string) (net.Listener, error)

type Dependencies struct {
	Listen    ListenFunc
	Service   Service
	Readiness Readiness
}

func Run(ctx context.Context, cfg Config, deps Dependencies) error {
	listener, err := deps.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	readyCleared := false
	deps.Readiness.Set(true)
	defer func() {
		if !readyCleared {
			deps.Readiness.Set(false)
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- deps.Service.Listener(listener)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		deps.Readiness.Set(false)
		readyCleared = true
		if err := deps.Service.ShutdownWithTimeout(cfg.ShutdownTimeout); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("serve after shutdown: %w", err)
		}
		return nil
	}
}
