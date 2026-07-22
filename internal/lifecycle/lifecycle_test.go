package lifecycle

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

var (
	errListen   = errors.New("listen failed")
	errShutdown = errors.New("shutdown deadline exceeded")
	errServe    = errors.New("serve failed")
)

func TestRunListenerFailure(t *testing.T) {
	ready := &recordingReadiness{}
	err := Run(context.Background(), Config{Address: ":8080"}, Dependencies{
		Listen:    func(string, string) (net.Listener, error) { return nil, errListen },
		Service:   &fakeService{},
		Readiness: ready,
	})
	if !errors.Is(err, errListen) {
		t.Fatalf("Run() error = %v, want listener error", err)
	}
	if got := ready.Values(); len(got) != 0 {
		t.Fatalf("readiness changed before listener opened: %v", got)
	}
}

func TestRunReadyTransitionOnSignalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := newFakeService(nil, nil)
	ready := &recordingReadiness{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Address: ":8080", ShutdownTimeout: 7 * time.Second}, Dependencies{
			Listen:    fakeListen,
			Service:   service,
			Readiness: ready,
		})
	}()
	<-service.started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := ready.Values(), []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("readiness transitions = %v, want %v", got, want)
	}
	if service.shutdownTimeout != 7*time.Second {
		t.Fatalf("shutdown timeout = %s, want 7s", service.shutdownTimeout)
	}
}

func TestRunSIGTERMPathReportsShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := newFakeService(errShutdown, nil)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Address: ":8080", ShutdownTimeout: time.Second}, Dependencies{
			Listen:    fakeListen,
			Service:   service,
			Readiness: &recordingReadiness{},
		})
	}()
	<-service.started
	cancel() // production cancellation is driven by SIGINT or SIGTERM
	if err := <-done; !errors.Is(err, errShutdown) {
		t.Fatalf("Run() error = %v, want shutdown timeout", err)
	}
}

func TestRunReportsServeErrorAfterShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := newFakeService(nil, errServe)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Address: ":8080", ShutdownTimeout: time.Second}, Dependencies{
			Listen:    fakeListen,
			Service:   service,
			Readiness: &recordingReadiness{},
		})
	}()
	<-service.started
	cancel()
	if err := <-done; !errors.Is(err, errServe) {
		t.Fatalf("Run() error = %v, want serve-after-shutdown error", err)
	}
}

type fakeService struct {
	started         chan struct{}
	serveDone       chan struct{}
	shutdownErr     error
	serveErr        error
	shutdownTimeout time.Duration
	once            sync.Once
}

func newFakeService(shutdownErr, serveErr error) *fakeService {
	return &fakeService{
		started:     make(chan struct{}),
		serveDone:   make(chan struct{}),
		shutdownErr: shutdownErr,
		serveErr:    serveErr,
	}
}

func (s *fakeService) Listener(net.Listener) error {
	close(s.started)
	<-s.serveDone
	return s.serveErr
}

func (s *fakeService) ShutdownWithTimeout(timeout time.Duration) error {
	s.shutdownTimeout = timeout
	s.once.Do(func() { close(s.serveDone) })
	return s.shutdownErr
}

type recordingReadiness struct {
	mu     sync.Mutex
	values []bool
}

func (r *recordingReadiness) Set(value bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, value)
}

func (r *recordingReadiness) Values() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.values...)
}

type fakeListener struct{}

func fakeListen(string, string) (net.Listener, error) { return fakeListener{}, nil }
func (fakeListener) Accept() (net.Conn, error)        { return nil, net.ErrClosed }
func (fakeListener) Close() error                     { return nil }
func (fakeListener) Addr() net.Addr                   { return fakeAddr("test") }

type fakeAddr string

func (a fakeAddr) Network() string { return string(a) }
func (a fakeAddr) String() string  { return string(a) }
