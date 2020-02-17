package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/damnever/goodog/frontend"
	"github.com/damnever/libext-go/rand"
	"github.com/stretchr/testify/require"

	// Plug in Caddy modules here
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/damnever/goodog/backend/caddy"
)

var caddyconfig = `{
  "admin": {
    "config": { "persist": false }
  },
  "apps": {
    "http": {
      "servers": {
        "goodog": {
          "automatic_https": {"disable": true},
          "experimental_http3": true,
          "listen": ["%s"],
          "read_timeout": "30s",
          "write_timeout": "10s",
          "routes": [
            {
              "match": [ {"path": ["/"]} ],
              "handle": [
                {
                  "handler": "authentication",
                  "providers": {
                    "http_basic": {
                      "hash": { "algorithm": "bcrypt" },
                      "realm": "restricted",
                      "accounts": [
                        {
                          "username": "knock",
                          "password": "JDJhJDEwJDJoRlRlUGt1NGdUMjRkV0EwNkpwVS4ucHZlcjQuWTZDSGR4S2Q5enFzTG5ESHdvT2xvSVZ1",
                          "salt": ""
                        }
                      ]
                    }
                  }
                },
                {
                  "handler": "goodog",
                  "upstream_tcp": "%s",
                  "upstream_udp": "%s",
                  "connect_timeout": "10s",
                  "read_timeout": "30s",
                  "write_timeout": "10s"
                }
              ],
              "terminal": true
            }
          ],
          "tls_connection_policies": [
            {
              "match": { "sni": [""] },
              "certificate_selection": {
                "policy": "custom",
                "tag": "goodog"
              }
            }
          ]
        }
      }
    },
    "tls": {
      "certificates": {
        "load_pem": [
          {
            "certificate": "%s",
            "key": "%s",
            "tags": ["goodog"]
          }
        ]
      }
    }
  }
}`

func findaddr(t *testing.T) string {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("findaddr: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestGoodog(t *testing.T) {
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
	// _, filename, _, _ := runtime.Caller(0)
	// curdir := filepath.Dir(filename)
	caddyconfig = fmt.Sprintf(caddyconfig, backendaddr, remoteaddr, remoteaddr,
		selfSignedCertificate, selfSignedKey)
	resp, err := http.Post("http://localhost:2019/load", "application/json",
		bytes.NewBufferString(caddyconfig))
	require.Nil(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Status)
	resp.Body.Close()

	t.Log("start frontend..")
	frontendaddr := findaddr(t)
	args := url.Values{
		"compression": []string{"snappy"},
		"version":     []string{"v1"},
	}
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
				value := rand.String(j)
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
				data := []byte(rand.String(222))
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
	time.Sleep(555 * time.Millisecond)
}

// Generated by the script in https://caddy.community/t/v2-comprehensive-guide-to-using-self-signed-certs/6763/8
var (
	selfSignedCertificate = strings.Join(strings.Split(`-----BEGIN CERTIFICATE-----
MIIDWTCCAkGgAwIBAgIJAMZce9j7gMBlMA0GCSqGSIb3DQEBCwUAMA0xCzAJBgNV
BAYTAkNOMCAXDTIwMDIxNjAzMjYxNloYDzIxMjAwMTIzMDMyNjE2WjANMQswCQYD
VQQGEwJDTjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAL3KBTfT9C84
g+6bsKY3BLKqbpmUjR2b8ZeDa/73zsun5wWGxXMcMYalrwzengMhkO9fb1GkdQte
wuon9LpK90t855YvOmd6p90xq/iiXaflIyObZoxd+QL7Fo4Ldi82emd1Wlm8C/WL
uqttLp0RdJM3G2XHt/pfI9xUpDnIpdatH+T3EIMTFuvftD7d7T90nDn0NSdScWKC
gXj7mh6zmtr7m6KyIeyPeNBdh6q8NeLEIrkQHy5eFX/TisNUIs5fI3V0raGlw79T
oS73/HIf4Y/soWAI0bHN0vi5WPEitTMh8/rlB8f/lFq7j5Etcdpkwgjjqs/Ll54a
YS79Pm5w270CAwEAAaOBuTCBtjAdBgNVHQ4EFgQU+oYw0iJecIOnJIyzEPipHye1
r0IwHwYDVR0jBBgwFoAU+oYw0iJecIOnJIyzEPipHye1r0IwCQYDVR0TBAIwADAL
BgNVHQ8EBAMCBeAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwGQYDVR0RBBIwEIIJMTI3
LjAuMC4xggM6OjEwLAYJYIZIAYb4QgENBB8WHU9wZW5TU0wgR2VuZXJhdGVkIENl
cnRpZmljYXRlMA0GCSqGSIb3DQEBCwUAA4IBAQAxoidlVlGdGtrkO4HILpGfRiwi
anOlbkBouuzsQjrE1lgzvTspquCGY+YhoDRwS8YD0B7fEPJAFkRGpSwSU1BPaqLx
2V+MmZ6reIFbAMkKYChIbGCNRJ0WVRmTICCTBh0K9WbcxzlzoYYVZ072zGwu4WiJ
4hAqMkyZDmG8a+4BNVwRaulHNO+WAOJJ3CGLLY+8/H7ITcwYsqcB/7B9HdB7Bj/Q
hb/KCJEDuaAgJ0zFdRF4ZijOfJe/JYLCRKdz5LFoJ2NlD6ymSURhc9yiZ8Bidjgj
ad2Xd0xW/3U2oS/H5URdXE8jjdy0HuhLOlkdgceK+FzDH4cOfYcJ8JE9Jfcb
-----END CERTIFICATE-----`, "\n"), "\\n")
	selfSignedKey = strings.Join(strings.Split(`-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC9ygU30/QvOIPu
m7CmNwSyqm6ZlI0dm/GXg2v+987Lp+cFhsVzHDGGpa8M3p4DIZDvX29RpHULXsLq
J/S6SvdLfOeWLzpneqfdMav4ol2n5SMjm2aMXfkC+xaOC3YvNnpndVpZvAv1i7qr
bS6dEXSTNxtlx7f6XyPcVKQ5yKXWrR/k9xCDExbr37Q+3e0/dJw59DUnUnFigoF4
+5oes5ra+5uisiHsj3jQXYeqvDXixCK5EB8uXhV/04rDVCLOXyN1dK2hpcO/U6Eu
9/xyH+GP7KFgCNGxzdL4uVjxIrUzIfP65QfH/5Rau4+RLXHaZMII46rPy5eeGmEu
/T5ucNu9AgMBAAECggEAI5OECOQNaPCiIo9CvNWhZtB17QogrcU2s10qWGAhfqGZ
t7p8tsg5LHFQcAwm+JVJMuXj2x0F57y6suQMhwNYeekPDGMMAqvGXbta7j+ZaMiW
Hq2ZuoQ/EmT45GWXoOAIb+5aommSoFOyCUJtM3o7LQFufFTE0wUUls+y/TX0iFoW
9RQLF7JpDfhpFBzER/SAL0CIZrw8aYOjHsCtTk5u7I+BzdS6DtAe+kFwTS7bYm3l
LrV9sV/nbS0Ytm/b+1YlwE5hyrQH/8Sh000Y7MSy4vWHJ4N2RwQOZQLReBt7fyBA
5z6LiQyYPN+pxu/EIPbfVPSXfaVG/SH1w4J9gtjdYQKBgQDelazvi1IC1lbPOOzR
Bi7PYSMCngZX907wzNu+fbr0ycz/NpGZmsoWXaKeMPqbOnxKYoaZ3d45B7RqF6h2
IVsazLd03ldymEVWQd9pFgaYZKFgfE5xx5+usUxRnNnsfbaQ3NTnfJC43wlqREU+
aPwCdd7+V4tScd41Nh1m9blw5QKBgQDaR/RVXGoFTT860y7DT921mK6yOoPGx15l
TKr5WEApTsAo6HAjOJLbEu1e/ckCwpf/axc2saK/teWIVKMvPsx0t71y69HK9RJ4
BZqE8T9IDQx8XI6joTJ3sZ0ycgrm9BCin2B701FSxfymfHLxGV3n+8uhMPJiQTMW
n7OUmBIJ+QKBgQCqRIr+z2+T9gyABka5+uXSA7d5aBLoNamwcLVkOd/LI5fqXv7w
JsWSaFxecI80MYAkksvuZhd5PtiXE7PtccS0coegIfl5EtxviIJza8LtzoTYPx7u
0MrpIn2ELN1TmDMRC6zdy58VnKAiJ0lk3YByDWLg420TS0G1KMlDGpOZtQKBgQC8
gv3hppEtiPv9eprdNKFeDsF4zQ43YsEELUVPWEb5JbjQ24TU9ivmJR95NSYfSx1o
Cf2fT6QlexsDNU1FJS//8RsdH8osRKCxpO1AuPSU7igFUw4hBLsIIg2HnnQJ52hi
edAiwGpwWOqMgdfmnqi6C3xd9l6uOm67sCqwPvD9SQKBgEhd2hNBx6RlHRLp6KEl
MSiOncRZp2R+Ghspj7SR3Udl2RdqOncPi/dvPh255U0GoeKsPK/OXErabH/7n0tJ
3CNEFb/0MBID3VG+ZOgiIsWBB1J9XKYhsVCi5+P69nhIHEcA+dVPyelUhhG0k2Z2
RaDCApZaI5neAfl0DDUZv/vl
-----END PRIVATE KEY-----`, "\n"), "\\n")
)
