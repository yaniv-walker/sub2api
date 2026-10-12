package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestEmailCancellationInterruptsSilentSMTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan struct{})
	done := make(chan struct{})
	releaseServer := make(chan struct{})
	defer func() { close(releaseServer); _ = listener.Close(); <-done }()
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		// The server must not close when the client context is cancelled.
		<-releaseServer
	}()
	config := &SMTPConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: "sender@example.com"}
	result := make(chan error, 1)
	go func() {
		result <- (&EmailService{}).sendEmailWithContext(ctx, config, "ops@example.com", "subject", "<p>body</p>")
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection was not accepted")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancelled SMTP send did not stop")
	}
}
