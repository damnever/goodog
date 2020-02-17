package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/damnever/goodog/frontend"
	randext "github.com/damnever/libext-go/rand"
	"github.com/stretchr/testify/require"

	// Plug in Caddy modules here
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/damnever/goodog/backend/caddy"
)

func TestGoodog(t *testing.T) {
	t.Run("no-compression", func(subt *testing.T) {
		testWithArgs(subt, url.Values{
			"version": []string{"v1"},
		})
	})
	time.Sleep(555 * time.Millisecond)

	t.Run("snappy-compression", func(subt *testing.T) {
		testWithArgs(subt, url.Values{
			"version":     []string{"v1"},
			"compression": []string{"snappy"},
		})
	})
}

func testWithArgs(t *testing.T, args url.Values) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Log("start tcp and udp server..")
	remoteaddr := findaddr(t)
	go tcpEchoServer(ctx, remoteaddr, t)
	go udpEchoServer(ctx, remoteaddr, t)

	t.Log("start backend-caddyv2..")
	os.Args = []string{"caddy", "run"}
	go caddycmd.Main()
	time.Sleep(33 * time.Millisecond)
	backendaddr := findaddr(t)
	resp, err := http.Post(
		"http://localhost:2019/load", "application/json",
		bytes.NewBufferString(caddyConfig(t, backendaddr, remoteaddr)),
	)
	require.Nil(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Status)
	resp.Body.Close()

	t.Log("start frontend..")
	frontendaddr := findaddr(t)
	proxy, err := frontend.NewProxy(frontend.Config{
		ListenAddr:         frontendaddr,
		ServerURI:          "https://knock:knock@" + backendaddr + "/?" + args.Encode(),
		Connector:          "caddy-http3",
		LogLevel:           "debug",
		InsecureSkipVerify: true,
		ConnectTimeout:     5 * time.Second,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       5 * time.Second,
	})
	require.Nil(t, err)
	defer proxy.Close()
	go proxy.Serve(ctx)

	t.Log("start clients..")
	time.Sleep(666 * time.Millisecond)

	wg := sync.WaitGroup{}
	for i := 0; i < 22; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := net.Dial("tcp", frontendaddr)
			require.Nil(t, err)
			defer conn.Close()
			var values []string
			for j := 22; j < 222; j++ {
				value := randext.String(j)
				_, err := conn.Write([]byte(value))
				require.Nil(t, err)
				values = append(values, value)
			}
			data := strings.Join(values, "")
			buf := make([]byte, len(data))
			_, err = io.ReadFull(conn, buf)
			require.Nil(t, err)
			require.Equal(t, data, string(buf))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()

			udpaddr, err := net.ResolveUDPAddr("udp", frontendaddr)
			require.Nil(t, err)
			conn, err := net.DialUDP("udp", nil, udpaddr)
			require.Nil(t, err)
			defer conn.Close()
			buf := make([]byte, 222, 222)
			for j := 22; j < 222; j++ {
				data := []byte(randext.String(222))
				_, err := conn.Write(data)
				require.Nil(t, err)
				n, err := conn.Read(buf)
				require.Nil(t, err)
				require.Equal(t, len(data), n)
				require.Equal(t, data, buf[:n])
			}
		}()
	}
	wg.Wait()
}

func findaddr(t *testing.T) string {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("findaddr: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func caddyConfig(t *testing.T, listenAddr string, upstream string) string {
	_, filename, _, _ := runtime.Caller(0)
	testdata := filepath.Join(filepath.Dir(filename), "testdata")
	config, err := ioutil.ReadFile(filepath.Join(testdata, "caddy.json"))
	require.Nil(t, err)
	certificate, err := ioutil.ReadFile(filepath.Join(testdata, "cert.pem"))
	require.Nil(t, err)
	private, err := ioutil.ReadFile(filepath.Join(testdata, "key.pem"))
	require.Nil(t, err)
	return fmt.Sprintf(
		string(config), listenAddr, upstream, upstream,
		strings.Replace(string(certificate), "\n", "\\n", -1),
		strings.Replace(string(private), "\n", "\\n", -1),
	)
}
