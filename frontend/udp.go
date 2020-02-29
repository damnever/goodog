package frontend

import (
	"context"
	"io"
	"math"
	"net"
	"sync"
	"time"

	"github.com/damnever/goctl/retry"
	bytesext "github.com/damnever/libext-go/bytes"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/damnever/goodog/internal/pkg/encoding"
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

	pendingUpstreams *counter
	upstreams        *counter
	idleClosed       *counter
	connectErrors    *counter
	readWriteErrors  *counter
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
		retrier:   retry.New(retry.ConstantBackoffs(2, 10*time.Millisecond)),
		ups:       map[string]*udpUpstreamWrapper{},

		pendingUpstreams: newCounter("udp.pending-upstreams"),
		upstreams:        newCounter("udp.upstreams"),
		idleClosed:       newCounter("udp.idle-timeouts"),
		connectErrors:    newCounter("udp.errors.connect"),
		readWriteErrors:  newCounter("udp.errors.read-write"),
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
		n, addr, err := p.conn.ReadFrom(buf)
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
	timeout := 10 * time.Second
	if p.conf.Timeout > timeout { // Is that ok?
		timeout = p.conf.Timeout
	}
	ticker := time.NewTicker(3 * time.Second)
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
				if count > 0 {
					p.logger.Info("idle check", zap.Int("closed", count))
					p.idleClosed.Add(uint32(count))
				}
			}
		}
	}
}

func (p *udpProxy) handle(ctx context.Context, downstreamAddr net.Addr, data []byte) {
	err := p.retrier.Run(ctx, func() (st retry.State, err0 error) {
		var upstream *udpUpstreamWrapper
		upstream, err0 = p.getRemoteWriter(ctx, downstreamAddr)
		if err0 != nil {
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
		p.readWriteErrors.Inc()
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

	p.pendingUpstreams.Inc()
	upstream, err := p.connector.Connect(ctx)
	if err != nil {
		p.pendingUpstreams.Dec()
		p.connectErrors.Inc()
		p.logger.Error("connect to upstream failed",
			zap.String("upstream", p.conf.ServerHost()),
			zap.String("downstream", downstreamAddr.String()),
			zap.Error(err),
		)
		return nil, err
	}
	p.pendingUpstreams.Dec()

	p.upmu.Lock()
	defer p.upmu.Unlock()
	if w, ok := p.ups[addrStr]; ok {
		upstream.Close()
		return w, nil
	}
	p.upstreams.Inc()

	upstream = tryWrapWithCompression(upstream, p.conf.Compression)
	upstreamWrapper := newUDPUpstreamWrapper(addrStr, upstream)
	p.ups[addrStr] = upstreamWrapper
	go p.serveAddr(ctx, downstreamAddr, upstreamWrapper)
	return upstreamWrapper, nil
}

func (p *udpProxy) serveAddr(_ context.Context, downstreamAddr net.Addr, upstream *udpUpstreamWrapper) {
	var (
		n   int
		err error
		buf = p.pool.Get(math.MaxUint16)
	)
	for {
		// TODO: timeout??
		if n, err = upstream.ReadPacket(buf); err != nil {
			p.readWriteErrors.Inc()
			break
		}
		if _, err = p.conn.WriteTo(buf[:n], downstreamAddr); err != nil {
			p.readWriteErrors.Inc()
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
	p.upstreams.Dec()
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
