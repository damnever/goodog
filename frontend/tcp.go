package frontend

import (
	"context"
	"io"
	"net"

	bytesext "github.com/damnever/libext-go/bytes"
	netext "github.com/damnever/libext-go/net"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

type tcpProxy struct {
	conf      Config
	logger    *zap.Logger
	connector Connector
	server    *netext.Server
	pool      *bytesext.Pool

	streams atomic.Uint32
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

func (p *tcpProxy) handle(ctx context.Context, downstream net.Conn) {
	upstream, err := p.connector.Connect(ctx)
	if err != nil {
		downstream.Close()
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstream.RemoteAddr().String()),
			zap.Error(err),
		)
		return
	}
	p.logger.Info("+stream",
		zap.Uint32("streams", p.streams.Inc()),
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstream.RemoteAddr().String()),
	)

	errc := make(chan error, 2)
	streamFunc := func(dst, src io.ReadWriter, msg string) {
		buf := p.pool.Get(8192)
		_, err := io.CopyBuffer(dst, src, buf)
		p.logger.Debug(msg,
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstream.RemoteAddr().String()),
			zap.Error(err),
		)
		p.pool.Put(buf)
		errc <- err
	}

	upstream = tryWrapWithCompression(upstream, p.conf.Compression)
	go streamFunc(downstream, upstream, "upstream->downstream done")
	go streamFunc(upstream, downstream, "downstream->upstream done")

	select {
	case <-ctx.Done():
	case <-errc:
	}
	upstream.Close()
	downstream.Close()
	p.logger.Info("-stream",
		zap.Uint32("streams", p.streams.Dec()),
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstream.RemoteAddr().String()),
	)
}
