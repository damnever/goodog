package frontend

import (
	"context"
	"io"
	"net"

	netext "github.com/damnever/libext-go/net"
	"go.uber.org/zap"

	goodogioutil "github.com/damnever/goodog/internal/pkg/ioutil"
)

type tcpProxy struct {
	conf      Config
	logger    *zap.Logger
	connector Connector
	server    *netext.Server

	downstreams     *counter
	upstreams       *counter
	connectErrors   *counter
	readWriteErrors *counter
}

func newTCPProxy(conf Config, connector Connector, logger *zap.Logger) (*tcpProxy, error) {
	p := &tcpProxy{
		conf:      conf,
		logger:    logger.Named("tcp"),
		connector: connector,

		downstreams:     newCounter("tcp.downstreams"),
		upstreams:       newCounter("tcp.upstreams"),
		connectErrors:   newCounter("tcp.errors.connect"),
		readWriteErrors: newCounter("tcp.errors.read-write"),
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

func (p *tcpProxy) handle(ctx context.Context, downstreamConn net.Conn) {
	p.downstreams.Inc()
	upstream, err := p.connector.Connect(ctx)
	if err != nil {
		p.connectErrors.Inc()
		p.downstreams.Dec()
		downstreamConn.Close()
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamConn.RemoteAddr().String()),
			zap.Error(err),
		)
		return
	}
	p.upstreams.Inc()

	errc := make(chan error, 2)
	streamFunc := func(dst, src io.ReadWriter, msg string) {
		_, err := goodogioutil.Copy(dst, src, false)
		p.logger.Debug(msg,
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamConn.RemoteAddr().String()),
			zap.Error(err),
		)
		if err != nil {
			p.readWriteErrors.Inc()
		}
		errc <- err
	}

	downstream := netext.NewTimedConn(downstreamConn, p.conf.Timeout, p.conf.Timeout)
	upstream = tryWrapWithCompression(upstream, p.conf.Compression)
	go streamFunc(downstream, upstream, "upstream->downstream done")
	go streamFunc(upstream, downstream, "downstream->upstream done")

	select {
	case <-ctx.Done():
	case <-errc:
	}
	upstream.Close()
	downstream.Close()
	p.downstreams.Dec()
	p.upstreams.Dec()
}
