package snappypool

import (
	"io"
	"sync"

	"github.com/golang/snappy"
)

var (
	_snappyReaderPool = &sync.Pool{
		New: func() interface{} {
			return snappy.NewReader(nil)
		},
	}
	_snappyWriterPool = &sync.Pool{
		New: func() interface{} {
			return snappy.NewBufferedWriter(nil)
		},
	}
)

func GetReader(r io.Reader) *snappy.Reader {
	snappyr := _snappyReaderPool.Get().(*snappy.Reader)
	snappyr.Reset(r)
	return snappyr
}

func PutReader(r *snappy.Reader) {
	if r == nil {
		return
	}
	r.Reset(nil)
	_snappyReaderPool.Put(r)
}

func GetWriter(w io.Writer) *snappy.Writer {
	snappyw := _snappyWriterPool.Get().(*snappy.Writer)
	snappyw.Reset(w)
	return snappyw
}

func PutWriter(w *snappy.Writer) {
	if w == nil {
		return
	}
	w.Reset(nil)
	_snappyWriterPool.Put(w)
}
