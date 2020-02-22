package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof" // Import pprof
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/damnever/goodog"
	"github.com/damnever/goodog/frontend"
)

var (
	flagset        = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flagVersion    = flagset.Bool("version", false, "Print the version")
	flagListenAddr = flagset.String("listen", ":59487", "The Listen address")
	flagServerURI  = flagset.String("server", "https://<DOMAIN>/?version=v1&compression=snappy", "The remote server URI")
	flagConnector  = flagset.String("connector", "caddy-http3", "The connector(backend) type: [caddy-http3]")
	flagLogLevel   = flagset.String("log-level", "info", "The log level: [debug, info, warn, error, panic, fatal]")
	flagTimeout    = flagset.Duration("timeout", 15*time.Second, "The socket timeout")
	flagPProfAddr  = flagset.String("pprof-addr", "", "The address to enable golang pprof server")
)

func main() {
	flagset.Parse(os.Args[1:])

	if *flagVersion {
		fmt.Println(goodog.VersionInfo())
		return
	}

	if *flagPProfAddr != "" {
		go func() {
			if err := http.ListenAndServe(*flagPProfAddr, nil); err != nil {
				fmt.Printf("pprof server exit abnormally: %v\n", err)
			}
		}()
	}

	proxy, err := frontend.NewProxy(frontend.Config{
		ListenAddr: *flagListenAddr,
		ServerURI:  *flagServerURI,
		Connector:  *flagConnector,
		LogLevel:   *flagLogLevel,
		Timeout:    *flagTimeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Init failed: %v", err)
		os.Exit(1)
	}
	defer proxy.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		if err := proxy.Serve(ctx); err != nil {
			errc <- err
		}
	}()

	sigc := make(chan os.Signal)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigc:
		cancel() // Graceful??
	case <-errc:
		os.Exit(1)
	}
}
