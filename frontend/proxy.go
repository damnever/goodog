package frontend

import (
	"context"
	"fmt"
	"net/url"
	"time"

	errorsext "github.com/damnever/libext-go/errors"
)

type Config struct {
	ListenAddr         string
	ServerURI          string
	Connector          string
	LogLevel           string
	InsecureSkipVerify bool // This is for testing purpose.
	ConnectTimeout     time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration

	Compression string
	serverURL   *url.URL
}

func (conf Config) ServerHost() string {
	return conf.serverURL.Host
}

func (conf *Config) resolve() error {
	u, err := url.Parse(conf.ServerURI) // Whatever
	if err != nil {
		return err
	}
	if conf.Compression == "" {
		conf.Compression = u.Query().Get("compression")
	}
	conf.serverURL = u
	return nil
}

func (conf Config) makeURI(protocol string) string {
	rawQuery := conf.serverURL.RawQuery

	q := conf.serverURL.Query()
	q.Add("protocol", protocol)
	if conf.Compression != "" {
		q.Set("compression", conf.Compression)
	}
	conf.serverURL.RawQuery = q.Encode()
	uri := conf.serverURL.String()

	conf.serverURL.RawQuery = rawQuery
	return uri
}

type Proxy struct {
	conf      Config
	tcpserver *tcpProxy
	udpserver *udpProxy
}

func NewProxy(conf Config) (*Proxy, error) {
	if err := (&conf).resolve(); err != nil {
		return nil, err
	}
	if conf.Connector != "caddy-http3" {
		return nil, fmt.Errorf("goodog/frontend: only caddy-http3 supported")
	}
	setDefaultLogLevel(conf.LogLevel)

	tcpconnector := newCaddyHTTP3Connector(conf.makeURI("tcp"), conf.InsecureSkipVerify, conf.ReadTimeout)
	tcpserver, err := newTCPProxy(conf, tcpconnector, DefaultLogger)
	if err != nil {
		return nil, err
	}
	udpconnector := newCaddyHTTP3Connector(conf.makeURI("udp"), conf.InsecureSkipVerify, conf.ReadTimeout)
	udpserver, err := newUDPProxy(conf, udpconnector, DefaultLogger)
	if err != nil {
		tcpserver.Close()
		return nil, err
	}

	return &Proxy{
		conf:      conf,
		tcpserver: tcpserver,
		udpserver: udpserver,
	}, nil
}

func (p *Proxy) Serve(ctx context.Context) error {
	logger := DefaultLogger.Sugar()
	errc := make(chan error, 2)
	go func() {
		logger.Infof("TCP listen at: %s", p.conf.ListenAddr)
		err := p.tcpserver.Serve(ctx)
		if err != nil {
			logger.Errorf("TCP server stopped with: %v", err)
		} else {
			logger.Errorf("TCP server stopped")
		}
		errc <- err
	}()
	go func() {
		logger.Infof("UDP listen at: %s", p.conf.ListenAddr)
		err := p.udpserver.Serve(ctx)
		if err != nil {
			logger.Errorf("UDP server stopped with: %v", err)
		} else {
			logger.Errorf("UDP server stopped")
		}
		errc <- err
	}()

	multierr := &errorsext.MultiErr{}
	for i := 0; i < 2; i++ {
		multierr.Append(<-errc)
	}
	return multierr.Err()
}

func (p *Proxy) Close() error {
	multierr := &errorsext.MultiErr{}
	multierr.Append(p.tcpserver.Close())
	multierr.Append(p.udpserver.Close())
	return multierr.Err()
}
