package caddy

import (
	"context"
	"io"
	"math"
	"net"

	bytesext "github.com/damnever/libext-go/bytes"
	errorsext "github.com/damnever/libext-go/errors"
	ioext "github.com/damnever/libext-go/io"
	netext "github.com/damnever/libext-go/net"
	"go.uber.org/zap"

	"github.com/damnever/goodog/internal/pkg/encoding"
	goodogioutil "github.com/damnever/goodog/internal/pkg/ioutil"
)

type forwarder struct {
	opts Options

	dialer        *net.Dialer
	logger        *zap.Logger
	udpBufferPool *bytesext.Pool
}

func newForwarder(logger *zap.Logger, opts Options) *forwarder {
	return &forwarder{
		opts:          opts,
		dialer:        &net.Dialer{Timeout: opts.ConnectTimeout},
		logger:        logger,
		udpBufferPool: bytesext.NewPoolWith(0, math.MaxUint16),
	}
}

func (f *forwarder) ForwardTCP(ctx context.Context, downstream io.ReadWriteCloser) error {
	upstreamConn, err := f.dialer.DialContext(ctx, "tcp", f.opts.UpstreamTCP)
	if err != nil {
		upstreamConn.Close()
		return err
	}
	upstream := netext.NewTimedConn(upstreamConn, f.opts.Timeout, f.opts.Timeout)

	errc := make(chan error, 2)
	go f.stream(downstream, upstream, errc)
	go f.stream(upstream, downstream, errc)

	return f.wait(ctx, upstreamConn.Close, downstream.Close, errc, 2)
}

func (f *forwarder) ForwardUDP(ctx context.Context, downstream io.ReadWriteCloser) error {
	upstreamConn, err := f.dialer.DialContext(ctx, "udp", f.opts.UpstreamUDP)
	if err != nil {
		downstream.Close()
		return err
	}
	upstream := netext.NewTimedConn(upstreamConn, f.opts.Timeout, f.opts.Timeout)

	errc := make(chan error, 2)
	go func() { // upstream -> downstream
		buf := f.udpBufferPool.Get(math.MaxUint16)
		var (
			n   int
			err error
		)
		for {
			if n, err = upstream.Read(buf); err != nil {
				break
			}
			if err = encoding.WriteU16SizedBytes(downstream, buf[:n]); err == nil {
				if f, ok := downstream.(ioext.Flusher); ok {
					err = f.Flush()
				}
			}
			if err != nil {
				break
			}
		}
		f.udpBufferPool.Put(buf)
		errc <- err
	}()
	go func() { // downstream -> upstream
		buf := f.udpBufferPool.Get(math.MaxUint16)
		var (
			n   int
			err error
		)
		for {
			if n, err = encoding.ReadU16SizedBytes(downstream, buf); err != nil {
				break
			}
			// NOTE: use of WriteTo with pre-connected connection
			if _, err = upstream.Write(buf[:n]); err != nil {
				break
			}
		}
		f.udpBufferPool.Put(buf)
		errc <- err
	}()

	return f.wait(ctx, upstreamConn.Close, downstream.Close, errc, 2)
}

func (f *forwarder) wait(ctx context.Context, upCloseFunc, downCloseFunc func() error, errc <-chan error, n int) error {
	donec := ctx.Done()
	multierr := &errorsext.MultiErr{}
	closed := false
	for n > 0 {
		select {
		case err := <-errc:
			n--
			multierr.Append(err)
		case <-donec:
			donec = nil
		}
		if !closed {
			closed = true
			_ = upCloseFunc()
			_ = downCloseFunc()
		}
	}
	if !closed {
		_ = upCloseFunc()
		_ = downCloseFunc()
	}
	return multierr.Err()
}

func (f *forwarder) stream(dst io.Writer, src io.Reader, errc chan error) {
	_, err := goodogioutil.Copy(dst, src, false)
	errc <- err
}
