package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof" // Import pprof
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard" // Plug in Caddy module
	randext "github.com/damnever/libext-go/rand"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	_ "github.com/damnever/goodog/backend/caddy" // Plug in Caddy module
	"github.com/damnever/goodog/frontend"
)

func TestGoodog(t *testing.T) {
	defer goleak.VerifyNone(t)

	go func() {
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Printf("pprof server exit abnormally: %v\n", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("start tcp and udp server..")
	remoteaddr := findaddr(t)
	go tcpEchoServer(ctx, remoteaddr, t)
	go udpEchoServer(ctx, remoteaddr, t)

	fmt.Println("start backend-caddyv2..")
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

	t.Run("no-compression", func(subt *testing.T) {
		testWithArgs(ctx, subt, backendaddr, url.Values{
			"version": []string{"v1"},
		})
	})

	t.Run("snappy-compression", func(subt *testing.T) {
		testWithArgs(ctx, subt, backendaddr, url.Values{
			"version":     []string{"v1"},
			"compression": []string{"snappy"},
		})
	})

	os.Args = []string{"caddy", "stop"}
	caddycmd.Main()
}

func testWithArgs(ctx context.Context, t *testing.T, backendaddr string, args url.Values) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	fmt.Println("start frontend..")
	frontendaddr := findaddr(t)
	proxy, err := frontend.NewProxy(frontend.Config{
		ListenAddr:         frontendaddr,
		ServerURI:          "https://knock:knock@" + backendaddr + "/?" + args.Encode(),
		Connector:          "caddy-http3",
		LogLevel:           "debug",
		InsecureSkipVerify: true,
		Timeout:            30 * time.Second,
	})
	require.Nil(t, err)
	defer proxy.Close()
	go proxy.Serve(ctx)

	fmt.Println("start clients..")
	time.Sleep(333 * time.Millisecond)

	wg := sync.WaitGroup{}
	for i := 0; i < 22; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := net.Dial("tcp", frontendaddr)
			require.Nil(t, err)
			defer conn.Close()
			var values []string
			for j := 22; j < 99; j++ {
				value := randext.String(j)
				_, err = conn.Write([]byte(value))
				require.Nil(t, err)
				values = append(values, value)
			}
			data := strings.Join(values, "")
			buf := make([]byte, len(data))
			_, err = io.ReadFull(conn, buf)
			require.Nil(t, err)
			require.Equal(t, data, string(buf))

			for j := 99; j < 222; j++ {
				value := randext.String(j)
				_, err := conn.Write([]byte(value))
				require.Nil(t, err)
				buf := make([]byte, len(value))
				_, err = io.ReadFull(conn, buf)
				require.Nil(t, err)
				require.Equal(t, value, string(buf))
			}
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
