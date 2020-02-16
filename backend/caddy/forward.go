package caddy

import (
	"context"
	"io"
	"math"
	"net"
	"time"

	"github.com/damnever/goodog/internal/pkg/encoding"
	errorsext "github.com/damnever/libext-go/errors"
	ioext "github.com/damnever/libext-go/io"
	netext "github.com/damnever/libext-go/net"
	"go.uber.org/zap"
)

type forwarder struct {
	opts Options

	dialer *net.Dialer
	logger *zap.Logger
}

func newForwarder(logger *zap.Logger, opts Options) *forwarder {
	return &forwarder{
		opts:   opts,
		dialer: &net.Dialer{Timeout: opts.ConnectTimeout},
		logger: logger,
	}
}

func (f *forwarder) ForwardTCP(ctx context.Context, downstream io.ReadWriter) error {
	conn, err := f.dialer.DialContext(ctx, "tcp", f.opts.UpstreamTCP)
	if err != nil {
		return err
	}
	defer conn.Close()
	upstream := netext.NewTimedConn(conn, f.opts.ReadTimeout, f.opts.WriteTimeout)

	errc := make(chan error, 2)
	go f.stream(downstream, upstream, errc)
	go f.stream(upstream, downstream, errc)

	return f.wait(ctx, errc, 2)
}

func (f *forwarder) ForwardUDP(ctx context.Context, downstream io.ReadWriter) error {
	upstreamAddr, err := net.ResolveUDPAddr("udp", f.opts.UpstreamUDP)
	if err != nil {
		return err
	}
	upstream, err := net.DialUDP("udp", nil, upstreamAddr)
	if err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() { // upstream -> downstream
		buf := make([]byte, math.MaxUint16, math.MaxUint16)
		for {
			upstream.SetReadDeadline(time.Now().Add(f.opts.ReadTimeout))
			n, err := upstream.Read(buf)
			if err != nil {
				errc <- err
				return
			}
			err = encoding.WriteU16SizedBytes(downstream, buf[:n])
			if err != nil {
				errc <- err
				return
			}
			if f, ok := downstream.(ioext.Flusher); ok {
				f.Flush()
			}
		}
	}()
	go func() {
		buf := make([]byte, math.MaxUint16, math.MaxUint16)
		for {
			n, err := encoding.ReadU16SizedBytes(downstream, buf)
			if err != nil {
				errc <- err
				return
			}
			upstream.SetWriteDeadline(time.Now().Add(f.opts.WriteTimeout))
			// NOTE: use of WriteTo with pre-connected connection
			if _, err := upstream.Write(buf[:n]); err != nil {
				errc <- err
				return
			}
		}
	}()

	return f.wait(ctx, errc, 2)
}

func (f *forwarder) wait(ctx context.Context, errc <-chan error, n int) error {
	multierr := &errorsext.MultiErr{}
	for i := 0; i < n; i++ {
		select {
		case err := <-errc:
			multierr.Append(err)
		case <-ctx.Done():
			return nil
		}
	}
	return multierr.Err()
}

func (f *forwarder) stream(dst io.Writer, src io.Reader, errc chan error) {
	_, err := io.Copy(dst, src)
	errc <- err
}
