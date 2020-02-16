package test

import (
	"context"
	"io"
	"net"
	"testing"

	netext "github.com/damnever/libext-go/net"
)

func tcpEchoServer(ctx context.Context, laddr string, t *testing.T) {
	server, err := netext.NewTCPServer(laddr, func(ctx context.Context, conn net.Conn) {
		defer conn.Close()
		if _, err := io.Copy(conn, conn); err != nil {
			t.Logf("tcp echo: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("tcp server init failed: %v", err)
	}

	go func() {
		<-ctx.Done()
		server.Close()
	}()
	server.Serve(netext.WithContext(ctx))
}

func udpEchoServer(ctx context.Context, laddr string, t *testing.T) {
	server, err := netext.NewUDPServer(laddr, func(_ context.Context, conn net.PacketConn, addr net.Addr, data []byte) {
		if _, err := conn.WriteTo(data, addr); err != nil {
			t.Logf("udp echo: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("udp server init failed: %v", err)
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	server.Serve(netext.WithContext(ctx))
}
