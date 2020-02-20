package frontend

import (
	"context"
	"io"
	"math"
	"net"
	"sync"

	"github.com/damnever/goodog/internal/pkg/encoding"
	bytesext "github.com/damnever/libext-go/bytes"
	"go.uber.org/zap"
)

type udpProxy struct {
	conf   Config
	logger *zap.Logger

	conn      net.PacketConn
	connector Connector
	pool      *bytesext.Pool

	upmu sync.RWMutex
	ups  map[string]io.WriteCloser
}

func newUDPProxy(conf Config, connector Connector, logger *zap.Logger) (*udpProxy, error) {
	conn, err := net.ListenPacket("udp", conf.ListenAddr)
	if err != nil {
		return nil, err
	}
	return &udpProxy{
		conf:      conf,
		logger:    logger.Named("udp"),
		conn:      conn,
		connector: connector,
		pool:      bytesext.NewPoolWith(0, math.MaxUint16),
		ups:       map[string]io.WriteCloser{},
	}, nil
}

func (p *udpProxy) Close() error {
	p.upmu.Lock()
	defer p.upmu.Unlock()
	for _, w := range p.ups {
		w.Close()
	}
	return p.conn.Close()
}

func (p *udpProxy) Serve(ctx context.Context) error {
	buf := make([]byte, math.MaxUint16, math.MaxUint16)
	for {
		n, addr, err := p.conn.ReadFrom(buf[:])
		if err != nil {
			return err
		}
		// NOTE(damnever): here assumes the underlying writer will make a copy of it.
		// data := make([]byte, n, n)
		// copy(data, buf[:n])
		p.handle(ctx, addr, buf[:n])
	}
}

func (p *udpProxy) handle(ctx context.Context, downstreamAddr net.Addr, data []byte) {
	reqw, err := p.getRemoteWriter(ctx, downstreamAddr)
	if err != nil {
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
		return
	}
	if err := encoding.WriteU16SizedBytes(reqw, data); err != nil {
		p.logger.Error("write to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
	}
}

func (p *udpProxy) getRemoteWriter(ctx context.Context, downstreamAddr net.Addr) (io.Writer, error) {
	addrStr := downstreamAddr.String()
	p.upmu.RLock()
	w, ok := p.ups[addrStr]
	p.upmu.RUnlock()
	if ok {
		return w, nil
	}

	upstream, err := p.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	p.upmu.Lock()
	defer p.upmu.Unlock()
	if w, ok := p.ups[addrStr]; ok {
		upstream.Close()
		return w, nil
	}
	upstream = tryWrapWithSafeCompression(upstream, p.conf.Compression)
	p.ups[addrStr] = upstream

	go func() {
		buf := p.pool.Get(math.MaxUint16)
		defer func() {
			p.pool.Put(buf)

			p.upmu.Lock()
			delete(p.ups, addrStr)
			p.upmu.Unlock()
			upstream.Close()
		}()

		for {
			n, err := encoding.ReadU16SizedBytes(upstream, buf)
			if err != nil {
				p.logger.Error("read from upstream failed",
					zap.String("upstream", p.conf.ServerHost()),
					zap.String("downstream", downstreamAddr.String()),
					zap.Error(err),
				)
				return
			}
			// TODO: timeout??
			if _, err := p.conn.WriteTo(buf[:n], downstreamAddr); err != nil {
				p.logger.Error("write to downstream failed",
					zap.String("upstream", p.conf.ServerHost()),
					zap.String("downstream", downstreamAddr.String()),
					zap.Error(err),
				)
				return
			}
		}
	}()
	return upstream, nil
}
