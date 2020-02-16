package frontend

import (
	"context"
	"io"
	"net"

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
	// FIXME(damnever): what a fucking mess..
	defer conn.Close()
	downstream := netext.NewTimedConn(conn, p.conf.ReadTimeout, p.conf.WriteTimeout)

	var downstreamReader io.Reader = downstream
	var reqwriter func()
	switch p.conf.Compression {
	case "snappy":
		r, w := io.Pipe()
		defer r.Close()
		downstreamReader = r

		reqwriter = func() {
			defer w.Close()
			snappyw := snappy.NewWriter(w)
			defer snappyw.Close()
			if _, err := io.Copy(snappyw, downstream); err != nil {
				p.logger.Error("streaming from downstream to upstream failed",
					zap.String("upstream", p.conf.ServerHost()),
					zap.String("downstream", conn.RemoteAddr().String()),
					zap.Error(err),
				)
			}
		}
	default:
	}

	upr, err := p.connector.Connect(ctx, downstreamReader)
	if err != nil {
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
		return
	}
	defer upr.Close()

	var upstreamReader io.Reader = upr
	switch p.conf.Compression {
	case "snappy":
		upstreamReader = snappy.NewReader(upr)
	default:
	}

	if reqwriter != nil {
		go reqwriter()
	}
	if _, err := io.Copy(downstream, upstreamReader); err != nil {
		p.logger.Error("streaming from upstream to downstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", conn.RemoteAddr().String()),
			zap.Error(err),
		)
	}
}
