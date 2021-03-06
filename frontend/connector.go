package frontend

import (
	"container/heap"
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
	url                string // e.g. goodog.x.io/?version=v1&protocol=tcp&compression=snappy
	insecureSkipVerify bool
	timeout            time.Duration

	mu      sync.Mutex
	clients *http3ClientsPriorityQueue
}

func newCaddyHTTP3Connector(serverURL string, insecureSkipVerify bool, timeout time.Duration) *caddyHTTP3Connector {
	return &caddyHTTP3Connector{
		url:                serverURL,
		insecureSkipVerify: insecureSkipVerify,
		timeout:            timeout,
		clients:            &http3ClientsPriorityQueue{},
	}
}

func (c *caddyHTTP3Connector) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	reqr, reqw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, reqr)
	if err != nil {
		reqr.Close()
		reqw.Close()
		return nil, err
	}
	// req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("User-Agent", "goodog/frontend")

	// TODO(damnever); connect timeout
	client := c.getClient()
	resp, err := client.Do(req)
	if err != nil {
		reqr.Close()
		reqw.Close()
		if resp != nil {
			_, _ = io.Copy(ioutil.Discard, resp.Body)
			resp.Body.Close()
		}
		c.release(client)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		reqr.Close()
		reqw.Close()
		_, _ = io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		c.release(client)
		return nil, fmt.Errorf("connect failed: %s", resp.Status)
	}

	once := sync.Once{}
	return &withReqResp{
		reqr:    reqr,
		reqw:    reqw,
		respr:   resp.Body,
		onClose: func() { once.Do(func() { c.release(client) }) },
	}, nil
}

func (c *caddyHTTP3Connector) Close() error {
	c.mu.Lock()
	for _, client := range *c.clients {
		client.CloseIdleConnections()
	}
	c.mu.Unlock()
	return nil
}

func (c *caddyHTTP3Connector) getClient() *http3ClientWrapper {
	// FIXME(damnever): magic number
	// Ref: https://github.com/lucas-clemente/quic-go/wiki/DoS-mitigations
	const maxStreamsPreConn = 66

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clients.Len() == 0 || (*c.clients)[0].streams >= maxStreamsPreConn {
		client := &http3ClientWrapper{Client: c.newHTTPClient()}
		heap.Push(c.clients, client)
		return client
	}
	client := (*c.clients)[0] // DO NOT pop it.
	client.streams++          // Increase the counter immediately to avoid bursting..
	heap.Fix(c.clients, client.index)
	return client
}

func (c *caddyHTTP3Connector) release(client *http3ClientWrapper) {
	c.mu.Lock()
	client.streams--
	heap.Fix(c.clients, client.index)
	c.mu.Unlock()
}

func (c *caddyHTTP3Connector) newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http3.RoundTripper{
			DisableCompression: true,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: c.insecureSkipVerify,
			},
			QuicConfig: &quic.Config{
				IdleTimeout: 6 * time.Minute,
				KeepAlive:   true,
			},
		},
		Timeout: c.timeout,
	}
}

type withReqResp struct {
	reqr    *io.PipeReader
	reqw    *io.PipeWriter
	rrmu    sync.Mutex
	respr   io.ReadCloser
	onClose func()
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
	_, _ = io.Copy(ioutil.Discard, rr.respr)
	err := rr.respr.Close()
	rr.rrmu.Unlock()
	rr.onClose()
	return err
}

type http3ClientWrapper struct {
	*http.Client
	streams int
	index   int
}

type http3ClientsPriorityQueue []*http3ClientWrapper

func (pq http3ClientsPriorityQueue) Len() int { return len(pq) }

func (pq http3ClientsPriorityQueue) Less(i, j int) bool { return pq[i].streams < pq[j].streams }

func (pq http3ClientsPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *http3ClientsPriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*http3ClientWrapper)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *http3ClientsPriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}
