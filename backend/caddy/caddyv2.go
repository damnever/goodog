package caddy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/damnever/goodog/internal/pkg/snappypool"
	ioext "github.com/damnever/libext-go/io"
	"go.uber.org/zap"
)

func init() {
	err := caddy.RegisterModule(&GoodogCaddyAdapter{})
	if err != nil {
		panic(err)
	}
	httpcaddyfile.RegisterHandlerDirective("goodog", parseCaddyfile)
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	g := &GoodogCaddyAdapter{}
	err := g.UnmarshalCaddyfile(h.Dispenser)
	return g, err
}

type GoodogCaddyAdapter struct {
	Options // For JSON config

	forwarder *forwarder
	logger    *zap.Logger
}

func (GoodogCaddyAdapter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.goodog",
		New: func() caddy.Module { return new(GoodogCaddyAdapter) },
	}
}

func (g *GoodogCaddyAdapter) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		args := d.RemainingArgs()
		if len(args) < 2 {
			continue
		}
		switch args[0] {
		case "upstream_tcp":
			g.Options.UpstreamTCP = args[1]
		case "upstream_udp":
			g.Options.UpstreamUDP = args[1]
		case "connect_timeout":
			d, err := time.ParseDuration(args[1])
			if err != nil {
				return err
			}
			g.Options.ConnectTimeout = d
		case "timeout":
			d, err := time.ParseDuration(args[1])
			if err != nil {
				return err
			}
			g.Options.Timeout = d
		}
	}
	return nil
}

func (g *GoodogCaddyAdapter) Provision(ctx caddy.Context) error {
	g.logger = ctx.Logger(g)
	(&g.Options).withDefaults()
	g.forwarder = newForwarder(g.logger, g.Options)
	g.logger.Info("goodog configured")
	return nil
}

func (g *GoodogCaddyAdapter) Validate() error {
	if g.forwarder == nil {
		return fmt.Errorf("goodog: not initialized")
	}
	if g.Options.UpstreamTCP == "" && g.Options.UpstreamUDP == "" {
		return fmt.Errorf("goodog: one of upstream_tcp or upstream_udp must be given")
	}
	return nil
}

func (g *GoodogCaddyAdapter) Cleanup() error {
	g.logger.Sync()
	return nil
}

func (g *GoodogCaddyAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.Method != http.MethodPost {
		next.ServeHTTP(w, r)
		return nil
	}

	args := r.URL.Query()
	if args.Get("version") != "v1" {
		w.WriteHeader(http.StatusBadRequest)
		r.Body.Close()
		return nil
	}

	rw := ioext.WithReadWriter{
		Reader: r.Body,
		Writer: w,
	}
	switch strings.ToLower(args.Get("compression")) {
	case "snappy":
		snappyr := snappypool.GetReader(r.Body)
		rw.Reader = snappyr
		snappyw := snappypool.GetWriter(w)
		rw.Writer = snappyw
		defer func() {
			snappypool.PutReader(snappyr)
			snappypool.PutWriter(snappyw)
		}()
	default:
	}

	switch strings.ToLower(args.Get("protocol")) {
	case "tcp":
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)
		return g.forwarder.ForwardTCP(r.Context(), ioext.WithCloser{
			ReadWriter: rw,
			Closer:     r.Body,
		})
	case "udp":
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)
		return g.forwarder.ForwardUDP(r.Context(), ioext.WithCloser{
			ReadWriter: rw,
			Closer:     r.Body,
		})
	default:
		w.WriteHeader(http.StatusBadRequest)
		r.Body.Close()
	}
	return nil
}
