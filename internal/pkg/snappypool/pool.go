package snappypool

import (
	"io"
	"sync"

	"github.com/golang/snappy"
)

var (
	snappyReaderPool = &sync.Pool{
		New: func() interface{} {
			return snappy.NewReader(nil)
		},
	}
	snappyWriterPool = &sync.Pool{
		New: func() interface{} {
			return snappy.NewWriter(nil)
		},
	}
)

func GetReader(r io.Reader) *snappy.Reader {
	snappyr := snappyReaderPool.Get().(*snappy.Reader)
	snappyr.Reset(r)
	return snappyr
}

func PutReader(r *snappy.Reader) {
	if r == nil {
		return
	}
	r.Reset(nil)
	snappyReaderPool.Put(r)
}

func GetWriter(w io.Writer) *snappy.Writer {
	snappyw := snappyWriterPool.Get().(*snappy.Writer)
	snappyw.Reset(w)
	return snappyw
}

func PutWriter(w *snappy.Writer) {
	if w == nil {
		return
	}
	w.Reset(nil)
	snappyWriterPool.Put(w)
}
