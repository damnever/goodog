package ioutil

import (
	"io"

	bytesext "github.com/damnever/libext-go/bytes"
	ioext "github.com/damnever/libext-go/io"
)

var _pool = bytesext.NewPoolWith(0, 32768)

// Copy is modified from io.copyBuffer, it pools the bytes buffers and
// flushes the writer after every write whenever possible.
func Copy(dst io.Writer, src io.Reader, flush bool) (written int64, err error) {
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	if wt, ok := src.(io.WriterTo); ok {
		return wt.WriteTo(dst)
	}
	// Similarly, if the writer has a ReadFrom method, use it to do the copy.
	if rt, ok := dst.(io.ReaderFrom); ok {
		return rt.ReadFrom(src)
	}

	buf := _pool.Get(32768)
	defer _pool.Put(buf)

	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew == nil && flush {
				if f, ok := dst.(ioext.Flusher); ok {
					ew = f.Flush()
				}
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}
