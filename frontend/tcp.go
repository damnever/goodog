package frontend

import (
	"context"
	"io"
	"net"

	ioext "github.com/damnever/libext-go/io"
	netext "github.com/damnever/libext-go/net"
	"github.com/golang/snappy"
	"go.uber.org/zap"
)

type tcpProxy struct {
	conf      Config
	logger    *zap.Logger
	connector Connector
	server    *netext.Server
}

func newTCPProxy(conf Config, connector Connector, logger *zap.Logger) (*tcpProxy, error) {
	p := &tcpProxy{
		conf:      conf,
		logger:    logger.Named("tcp"),
		connector: connector,
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

	upstreamRWC, err := p.connector.Connect(ctx)
	if err != nil {
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
		return
	}
	defer upstreamRWC.Close()

	upstream := ioext.WithReadWriter{
		Reader: upstreamRWC,
		Writer: upstreamRWC,
	}
	switch p.conf.Compression {
	case "snappy":
		upstream.Reader = snappy.NewReader(upstreamRWC)
		snappyw := snappy.NewWriter(upstreamRWC)
		// defer snappyw.Close() // XXX(damnever): no need to call it
		upstream.Writer = snappyw
	default:
	}

	donec := make(chan struct{}) // NOTE(damnever): fix the unknown data race
	go func() {
		if _, err := io.Copy(downstream, upstream); err != nil {
			p.logger.Error("streaming from upstream to downstream failed",
				zap.String("upstream", p.conf.ServerHost()),
				zap.String("downstream", conn.RemoteAddr().String()),
				zap.Error(err),
			)
		}
		close(donec)
	}()
	if _, err := io.Copy(upstream, downstream); err != nil {
		p.logger.Error("streaming from downstream to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
	}
	<-donec
}
