package frontend

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
)

type Connector interface {
	// The input io.Reader can be io.PipeReader and CloseWithError(io.EOF)
	Connect(context.Context, io.Reader) (io.ReadCloser, error)
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

func (c *caddyHTTP3Connector) Connect(ctx context.Context, in io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodPost, c.url, in)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Transfer-Encoding", "chunked")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// Does this apply to http3 lib?
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("connect failed: %s", resp.Status)
	}
	return resp.Body, nil
}

func (c *caddyHTTP3Connector) Close() error {
	c.client.CloseIdleConnections()
	return nil
}
