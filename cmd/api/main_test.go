package main

import (
	"context"
	"os"
	"reflect"
	"syscall"
	"testing"
)

func TestServiceContextRegistersSIGTERM(t *testing.T) {
	original := notifyContext
	defer func() { notifyContext = original }()

	var got []os.Signal
	notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		got = append(got, signals...)
		return context.WithCancel(parent)
	}
	_, stop := serviceContext()
	stop()

	want := []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered signals = %v, want %v", got, want)
	}
}
