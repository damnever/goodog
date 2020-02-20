package frontend

import (
	"context"
	"io"
	"net"

	bytesext "github.com/damnever/libext-go/bytes"
	netext "github.com/damnever/libext-go/net"
	"go.uber.org/zap"
)

type tcpProxy struct {
	conf      Config
	logger    *zap.Logger
	connector Connector
	server    *netext.Server
	pool      *bytesext.Pool
}

func newTCPProxy(conf Config, connector Connector, logger *zap.Logger) (*tcpProxy, error) {
	p := &tcpProxy{
		conf:      conf,
		logger:    logger.Named("tcp"),
		connector: connector,
		pool:      bytesext.NewPoolWith(0, 8192),
	}
	server, err := netext.NewTCPServer(conf.ListenAddr, p.handle)
	if err != nil {
		return nil, err
	}
	p.server = server
	return p, nil
}

func (p *tcpProxy) Serve(ctx context.Context) error {
	return p.server.Serve(netext.WithContext(ctx))
}

func (p *tcpProxy) Close() error {
	return p.server.Close()
}

func (p *tcpProxy) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	downstream := netext.NewTimedConn(conn, p.conf.ReadTimeout, p.conf.WriteTimeout)

	upstream, err := p.connector.Connect(ctx)
	if err != nil {
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
		return
	}
	defer upstream.Close()
	upstream = tryWrapWithCompression(upstream, p.conf.Compression)

	waitc := make(chan struct{}) // NOTE(damnever): fix the unknown data race
	go func() {
		buf := p.pool.Get(8192)
		defer p.pool.Put(buf)

		if _, err := io.CopyBuffer(downstream, upstream, buf); err != nil {
			p.logger.Error("streaming from upstream to downstream failed",
				zap.String("upstream", p.conf.ServerHost()),
				zap.String("downstream", conn.RemoteAddr().String()),
				zap.Error(err),
			)
		}
		close(waitc)
	}()

	buf := p.pool.Get(8192)
	defer p.pool.Put(buf)

	if _, err := io.CopyBuffer(upstream, downstream, buf); err != nil {
		p.logger.Error("streaming from downstream to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
	}

	select {
	case <-ctx.Done():
		conn.Close()
		<-waitc
	case <-waitc:
		return
	}
}
