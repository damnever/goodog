package frontend

import (
	"fmt"
	"io"
	"sync"

	"github.com/damnever/goodog/internal/pkg/snappypool"
	"github.com/golang/snappy"
)

func tryWrapWithCompression(rwc io.ReadWriteCloser, compressMethod string) io.ReadWriteCloser {
	switch compressMethod {
	default:
		return rwc
	case "snappy":
		return &withCompression{
			closer: rwc,
			reader: snappypool.GetReader(rwc),
			writer: snappypool.GetWriter(rwc),
		}
	}
}

func tryWrapWithSafeCompression(rwc io.ReadWriteCloser, compressMethod string) io.ReadWriteCloser {
	switch compressMethod {
	default:
		return rwc
	case "snappy":
		return &withCompression{
			closer: rwc,
			rmu:    &sync.Mutex{},
			reader: snappypool.GetReader(rwc),
			wmu:    &sync.Mutex{},
			writer: snappypool.GetWriter(rwc),
		}
	}
}

var (
	errReaderClosed = fmt.Errorf("goodog/frontend: reader closed")
	errWriterClosed = fmt.Errorf("goodog/frontend: writer closed")
)

type withCompression struct {
	closer io.Closer

	rmu    *sync.Mutex
	reader io.Reader
	wmu    *sync.Mutex
	writer io.Writer
}

func (c *withCompression) Read(p []byte) (n int, err error) {
	c.trylock(c.rmu)
	if c.reader != nil {
		n, err = c.reader.Read(p)
	} else {
		err = errReaderClosed
	}
	c.tryunlock(c.rmu)
	return
}

func (c *withCompression) Write(p []byte) (n int, err error) {
	c.trylock(c.wmu)
	if c.writer != nil {
		n, err = c.writer.Write(p)
	} else {
		err = errWriterClosed
	}
	c.tryunlock(c.wmu)
	return
}

func (c *withCompression) Close() error {
	err := c.closer.Close()

	c.trylock(c.rmu)
	if c.reader != nil {
		if snappyr, ok := c.reader.(*snappy.Reader); ok {
			snappypool.PutReader(snappyr)
		}
		c.reader = nil
	}
	c.tryunlock(c.rmu)

	c.trylock(c.wmu)
	if c.writer != nil {
		if snappyw, ok := c.writer.(*snappy.Writer); ok {
			snappypool.PutWriter(snappyw)
		}
		c.writer = nil
	}
	c.tryunlock(c.wmu)
	return err
}

func (c *withCompression) trylock(mu *sync.Mutex) {
	if mu != nil {
		mu.Lock()
	}
}

func (c *withCompression) tryunlock(mu *sync.Mutex) {
	if mu != nil {
		mu.Unlock()
	}
}
