package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerAppliesConnectionLimits(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", http.NewServeMux())
	if server.ReadHeaderTimeout != serverReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, serverReadHeaderTimeout)
	}
	if server.IdleTimeout != serverIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, serverIdleTimeout)
	}
	if server.MaxHeaderBytes != serverMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, serverMaxHeaderBytes)
	}
	if server.ReadTimeout != 0 || server.WriteTimeout != 0 {
		t.Fatal("large upload and download bodies must not inherit a short whole-request timeout")
	}
}

func TestServeHTTPServerWaitsForActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "ok")
	})
	server := newHTTPServer("127.0.0.1:0", handler)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveHTTPServer(ctx, server, listener) }()

	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			requestErr = response.Body.Close()
		}
		requestDone <- requestErr
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("server stopped before active request completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete")
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serveHTTPServer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down")
	}
}
