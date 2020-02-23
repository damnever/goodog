package frontend

import (
	"context"
	"io"
	"math"
	"net"
	"sync"
	"time"

	"github.com/damnever/goctl/retry"
	"github.com/damnever/goodog/internal/pkg/encoding"
	bytesext "github.com/damnever/libext-go/bytes"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

// udpProxy can not guarantee every single packet will be written to backend.
//
// IDEA: we could send UDP packet like this `| addr | size | data |`, so that
// there is no need to keep checking stream if it is idle, just maintains a pool
// of upstream streams, but this requires backend to keep tracking the address
// related packet, this way looks same to me and it consumes more bandwidth.
type udpProxy struct {
	conf   Config
	logger *zap.Logger

	conn      net.PacketConn
	connector Connector
	pool      *bytesext.Pool

	retrier retry.Retrier
	upmu    sync.RWMutex
	ups     map[string]*udpUpstreamWrapper
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
		retrier:   retry.New(retry.ZeroBackoffs(2)),
		ups:       map[string]*udpUpstreamWrapper{},
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
	go p.timeoutLoop(ctx)

	buf := make([]byte, math.MaxUint16, math.MaxUint16)
	for {
		n, addr, err := p.conn.ReadFrom(buf[:])
		if err != nil {
			return err
		}

		data := p.pool.Get(n)
		copy(data, buf[:n])
		go func(data []byte) {
			// FIXME(damnever): try to resuse this goroutine?
			p.handle(ctx, addr, data)
			p.pool.Put(data)
		}(data)
	}
}

func (p *udpProxy) timeoutLoop(ctx context.Context) {
	// FIXME(damnever): magic number
	const timeout = 16 * time.Second
	ticker := time.NewTicker(timeout / 4)
	defer ticker.Stop()
	addrs := []string{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timedout := time.Now().Add(-timeout)
			p.upmu.RLock()
			for _, up := range p.ups {
				if !up.ActiveAt().After(timedout) {
					addrs = append(addrs, up.Addr())
				}
			}
			p.upmu.RUnlock()

			if len(addrs) > 0 {
				count := 0
				p.upmu.Lock()
				for _, addr := range addrs {
					up, ok := p.ups[addr]
					if ok && !up.ActiveAt().After(timedout) {
						up.Close()
						delete(p.ups, addr)
						count++
					}
				}
				p.upmu.Unlock()
				addrs = addrs[:0]
				p.logger.Info("idle check", zap.Int("closed", count))
			}
		}
	}
}

func (p *udpProxy) handle(ctx context.Context, downstreamAddr net.Addr, data []byte) {
	err := p.retrier.Run(ctx, func() (st retry.State, err0 error) {
		var upstream *udpUpstreamWrapper
		upstream, err0 = p.getRemoteWriter(ctx, downstreamAddr)
		if err0 != nil {
			p.logger.Error("connect to upstream failed",
				zap.String("upstream", p.conf.ServerHost()),
				zap.String("downstream", downstreamAddr.String()),
				zap.Error(err0),
			)
			return
		}

		if err0 = upstream.WritePacket(data); err0 != nil {
			upstream.Close()
			p.upmu.Lock()
			delete(p.ups, downstreamAddr.String())
			p.upmu.Unlock()
		}
		return
	})
	if err != nil {
		p.logger.Debug("downstream->upstream done",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
	}
}

func (p *udpProxy) getRemoteWriter(ctx context.Context, downstreamAddr net.Addr) (*udpUpstreamWrapper, error) {
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
	upstreamWrapper := newUDPUpstreamWrapper(addrStr, upstream)
	p.ups[addrStr] = upstreamWrapper
	p.logger.Info("+stream",
		zap.Uint32("streams", p.streams.Inc()),
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstreamAddr.String()),
	)
	go p.serveAddr(ctx, downstreamAddr, upstreamWrapper)
	return upstreamWrapper, nil
}

func (p *udpProxy) serveAddr(ctx context.Context, downstreamAddr net.Addr, upstream *udpUpstreamWrapper) {
	var (
		n   int
		err error
		buf = p.pool.Get(math.MaxUint16)
	)
	for {
		// TODO: timeout??
		n, err = upstream.ReadPacket(buf)
		if err != nil {
			break
		}
		if _, err = p.conn.WriteTo(buf[:n], downstreamAddr); err != nil {
			break
		}
	}
	p.pool.Put(buf)
	p.logger.Debug("upstream->downstream done",
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstreamAddr.String()),
		zap.Error(err),
	)

	p.upmu.Lock()
	delete(p.ups, downstreamAddr.String())
	p.upmu.Unlock()
	upstream.Close()
	p.logger.Info("-stream",
		zap.Uint32("streams", p.streams.Dec()),
		zap.String("upstream", p.conf.ServerHost()),
		zap.String("downstream", downstreamAddr.String()),
	)
}

type udpUpstreamWrapper struct {
	addr     string
	activeAt atomic.Value

	upstream io.ReadWriteCloser
}

func newUDPUpstreamWrapper(addr string, upstream io.ReadWriteCloser) *udpUpstreamWrapper {
	u := &udpUpstreamWrapper{addr: addr, upstream: upstream}
	u.activeAt.Store(time.Now())
	return u
}

func (u *udpUpstreamWrapper) Addr() string {
	return u.addr
}

func (u *udpUpstreamWrapper) ReadPacket(p []byte) (int, error) {
	n, err := encoding.ReadU16SizedBytes(u.upstream, p)
	if err == nil {
		u.activeAt.Store(time.Now())
	}
	return n, err
}

func (u *udpUpstreamWrapper) WritePacket(p []byte) error {
	err := encoding.WriteU16SizedBytes(u.upstream, p)
	if err == nil {
		u.activeAt.Store(time.Now())
	}
	return err
}

func (u *udpUpstreamWrapper) ActiveAt() time.Time {
	return u.activeAt.Load().(time.Time)
}

func (u *udpUpstreamWrapper) Close() error {
	return u.upstream.Close()
}
