package frontend

import (
	"context"
	"io"
	"math"
	"net"
	"sync"

	"github.com/damnever/goodog/internal/pkg/encoding"
	"github.com/golang/snappy"
	"go.uber.org/zap"
)

type udpProxy struct {
	conf   Config
	logger *zap.Logger

	conn      net.PacketConn
	connector Connector

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
		data := make([]byte, n, n)
		copy(data, buf[:n])
		p.handle(ctx, addr, data)
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
	// FIXME(damnever): what a fucking mess..
	addrStr := downstreamAddr.String()
	p.upmu.RLock()
	w, ok := p.ups[addrStr]
	p.upmu.RUnlock()
	if ok {
		return w, nil
	}

	upreqrPipe, upreqwPipe := io.Pipe()
	uprespr, err := p.connector.Connect(ctx, upreqrPipe)
	if err != nil {
		return nil, err
	}
	var upreqw io.WriteCloser = upreqwPipe
	switch p.conf.Compression {
	case "snappy":
		upreqw = newSnappyWriterWrapper(upreqwPipe)
	default:
	}

	p.upmu.Lock()
	defer p.upmu.Unlock()
	if w, ok := p.ups[addrStr]; ok {
		upreqw.Close()
		uprespr.Close()
		return w, nil
	}
	p.ups[addrStr] = upreqw

	go func() {
		defer func() {
			p.upmu.Lock()
			delete(p.ups, addrStr)
			p.upmu.Unlock()
			upreqrPipe.Close()
			uprespr.Close()
		}()

		var upstreamr io.Reader = uprespr
		switch p.conf.Compression {
		case "snappy":
			upstreamr = snappy.NewReader(uprespr)
		default:
		}
		buf := make([]byte, math.MaxUint16, math.MaxUint16)
		for {
			n, err := encoding.ReadU16SizedBytes(upstreamr, buf)
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
	return upreqw, nil
}

type snappyWriterWrapper struct {
	origin  io.WriteCloser
	snappyw *snappy.Writer
}

func newSnappyWriterWrapper(origin io.WriteCloser) snappyWriterWrapper {
	return snappyWriterWrapper{
		origin:  origin,
		snappyw: snappy.NewWriter(origin),
	}
}

func (w snappyWriterWrapper) Write(p []byte) (int, error) {
	return w.snappyw.Write(p)
}

func (w snappyWriterWrapper) Close() error {
	// multierr.Append(w.snappyw.Close()) // FIXME(damnever): race
	return w.origin.Close() // XXX: There is no need to close for UDP..
}
