package service

import (
	"context"
	"net"
	"net/smtp"
)

// Closing the socket interrupts SMTP greeting, STARTTLS and message IO on shutdown.
// Preserve the concrete *tls.Conn so net/smtp recognizes encrypted connections.
func newSMTPClientContext(ctx context.Context, conn net.Conn, host string) (*smtp.Client, func(), error) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	client, err := newSMTPClient(conn, host)
	if err != nil {
		stop()
		return nil, nil, err
	}
	return client, func() { stop() }, nil
}
