package frontend

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
)

type Connector interface {
	Connect(context.Context) (io.ReadWriteCloser, error)
	Close() error
}

type caddyHTTP3Connector struct {
	url    string // e.g. goodog.x.io/?version=v1&protocol=tcp&compression=snappy
	client *http.Client
}

func newCaddyHTTP3Connector(serverURL string, insecureSkipVerify bool, timeout time.Duration) *caddyHTTP3Connector {
	return &caddyHTTP3Connector{
		url: serverURL,
		client: &http.Client{
			Transport: &http3.RoundTripper{
				DisableCompression: true,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: insecureSkipVerify,
				},
				QuicConfig: &quic.Config{
					IdleTimeout: 18 * time.Minute,
					KeepAlive:   true,
				},
			},
			Timeout: timeout,
		},
	}
}

func (c *caddyHTTP3Connector) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	reqr, reqw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, reqr)
	if err != nil {
		return nil, err
	}
	// req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("User-Agent", "goodog/frontend")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		reqr.Close()
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("connect failed: %s", resp.Status)
	}
	return &withReqResp{reqr: reqr, reqw: reqw, respr: resp.Body}, nil
}

func (c *caddyHTTP3Connector) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

type withReqResp struct {
	reqr  *io.PipeReader
	reqw  *io.PipeWriter
	rrmu  sync.Mutex
	respr io.ReadCloser
}

func (rr *withReqResp) Read(p []byte) (int, error) {
	rr.rrmu.Lock()
	n, err := rr.respr.Read(p)
	rr.rrmu.Unlock()
	return n, err
}

func (rr *withReqResp) Write(p []byte) (int, error) {
	return rr.reqw.Write(p)
}

func (rr *withReqResp) Close() error {
	rr.reqr.Close()
	rr.reqw.Close()
	rr.rrmu.Lock() // Fix potential data race
	io.Copy(ioutil.Discard, rr.respr)
	err := rr.respr.Close()
	rr.rrmu.Unlock()
	return err
}
