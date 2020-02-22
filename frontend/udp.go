package frontend

import (
	"context"
	"io"
	"math"
	"net"
	"sync"

	"github.com/damnever/goodog/internal/pkg/encoding"
	bytesext "github.com/damnever/libext-go/bytes"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

type udpProxy struct {
	conf   Config
	logger *zap.Logger

	conn      net.PacketConn
	connector Connector
	pool      *bytesext.Pool

	upmu    sync.RWMutex
	ups     map[string]io.WriteCloser
	streams atomic.Uint32
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
		pool:      bytesext.NewPoolWith(7, 512), // Max: math.MaxUint16
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

		data := p.pool.Get(n)
		copy(data, buf[:n])
		go func(data []byte) {
			// FIXME(damnever): resuse this goroutine
			p.handle(ctx, addr, data)
			p.pool.Put(data)
		}(data)
	}
}

func (p *udpProxy) handle(ctx context.Context, downstreamAddr net.Addr, data []byte) {
	upstream, err := p.getRemoteWriter(ctx, downstreamAddr)
	if err != nil {
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
		return
	}

	err = encoding.WriteU16SizedBytes(upstream, data)
	logf := p.logger.Info
	if err != nil {
		upstream.Close()
		p.upmu.Lock()
		delete(p.ups, downstreamAddr.String())
		p.upmu.Unlock()

		logf = p.logger.Warn
	}
	logf("downstream->upstream done",
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstreamAddr.String()),
		zap.Error(err),
	)
}

func (p *udpProxy) getRemoteWriter(ctx context.Context, downstreamAddr net.Addr) (io.WriteCloser, error) {
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
	upstream = tryWrapWithCompression(upstream, p.conf.Compression)
	p.ups[addrStr] = upstream

	p.logger.Info("+stream",
		zap.Uint32("streams", p.streams.Inc()),
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", addrStr),
	)
	go func() {
		var (
			n   int
			err error
			buf = p.pool.Get(math.MaxUint16)
		)
		for {
			// TODO: timeout??
			n, err = encoding.ReadU16SizedBytes(upstream, buf)
			if err != nil {
				break
			}
			if _, err = p.conn.WriteTo(buf[:n], downstreamAddr); err != nil {
				break
			}
		}
		p.pool.Put(buf)

		p.upmu.Lock()
		delete(p.ups, addrStr)
		p.upmu.Unlock()
		upstream.Close()

		logf := p.logger.Info
		if err != nil {
			logf = p.logger.Warn
		}
		logf("upstream->downstream done",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
		p.logger.Info("-stream",
			zap.Uint32("streams", p.streams.Dec()),
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", addrStr),
		)
	}()
	return upstream, nil
}
