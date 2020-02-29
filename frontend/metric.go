package frontend

import (
	"expvar"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/atomic"
)

var (
	metricMu = sync.Mutex{}
	metric   = metricMap{}
)

func init() {
	expvar.Publish("goodog-frontend", metric)
}

type metricMap map[string]expvar.Var

func (m metricMap) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "{")
	first := true
	for key, value := range m {
		if !first {
			fmt.Fprintf(&b, ", ")
		}
		fmt.Fprintf(&b, "%q: %v", key, value)
		first = false
	}
	fmt.Fprintf(&b, "}")
	return b.String()
}

type counter struct {
	atomic.Uint32
}

func (c *counter) String() string {
	return strconv.Itoa(int(c.Load()))
}

func newCounter(name string) *counter {
	parts := strings.Split(name, ".")
	n := len(parts)

	metricMu.Lock()
	defer metricMu.Unlock()
	next := metric
	for _, part := range parts[:n-1] {
		m, ok := next[part].(metricMap)
		if !ok {
			m = metricMap{}
			next[part] = m
		}
		next = m
	}

	c := &counter{}
	next[parts[n-1]] = c
	return c
}
