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

var (
	errReaderClosed = fmt.Errorf("goodog/frontend: reader closed")
	errWriterClosed = fmt.Errorf("goodog/frontend: writer closed")
)

type withCompression struct {
	closer io.Closer

	rmu    sync.Mutex
	reader io.Reader
	wmu    sync.Mutex
	writer io.Writer
}

func (c *withCompression) Read(p []byte) (n int, err error) {
	c.rmu.Lock()
	if c.reader != nil {
		n, err = c.reader.Read(p)
	} else {
		err = errReaderClosed
	}
	c.rmu.Unlock()
	return
}

func (c *withCompression) Write(p []byte) (n int, err error) {
	c.wmu.Lock()
	if c.writer != nil {
		n, err = c.writer.Write(p)
	} else {
		err = errWriterClosed
	}
	c.wmu.Unlock()
	return
}

func (c *withCompression) Close() error {
	err := c.closer.Close()

	c.rmu.Lock()
	if c.reader != nil {
		if snappyr, ok := c.reader.(*snappy.Reader); ok {
			snappypool.PutReader(snappyr)
		}
		c.reader = nil
	}
	c.rmu.Unlock()

	c.wmu.Lock()
	if c.writer != nil {
		if snappyw, ok := c.writer.(*snappy.Writer); ok {
			snappypool.PutWriter(snappyw)
		}
		c.writer = nil
	}
	c.wmu.Unlock()
	return err
}
