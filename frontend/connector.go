package frontend

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	errorsext "github.com/damnever/libext-go/errors"
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
	req, err := http.NewRequest(http.MethodPost, c.url, reqr)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("User-Agent", "goodog/frontend")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		reqr.Close()
		// Does this apply to http3 lib?
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("connect failed: %s", resp.Status)
	}
	return withReqResp{reqw: reqw, respr: resp.Body}, nil
}

func (c *caddyHTTP3Connector) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

type withReqResp struct {
	reqw  *io.PipeWriter
	respr io.ReadCloser
}

func (rr withReqResp) Read(p []byte) (int, error) {
	return rr.respr.Read(p)
}

func (rr withReqResp) Write(p []byte) (int, error) {
	return rr.reqw.Write(p)
}

func (rr withReqResp) Close() error {
	multierr := &errorsext.MultiErr{}
	multierr.Append(rr.reqw.Close())
	multierr.Append(rr.respr.Close())
	return multierr.Err()
}
