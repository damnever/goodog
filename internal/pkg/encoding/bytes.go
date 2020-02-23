package encoding

import (
	"encoding/binary"
	"io"
)

// NOTE(damnever): the following two functions is used to send/receive UDP packet,
// the maximum UDP packet is limited by IP packet and other conditions, so 16bit:
//   - https://en.wikipedia.org/wiki/IPv4#Header
//   - https://en.wikipedia.org/wiki/IPv6_packet#Fixed_header
// Also, there is a Ethernet frame size (MTU) causes packet fragmentation, that is
// what I do not care..
// Here using uint16 for simplicity, and varint/uvarint is a good choice though.

func ReadU16SizedBytes(r io.Reader, p []byte) (int, error) {
	var u16 uint16
	if err := binary.Read(r, binary.BigEndian, &u16); err != nil {
		return 0, err
	}
	n := int(u16)
	if n > len(p) {
		panic("goodog/pkg/encoding: size too small")
	}
	p = p[:n]
	return io.ReadFull(r, p)
}

func WriteU16SizedBytes(w io.Writer, data []byte) error {
	// XXX(damnever): make them a large one?
	u16 := uint16(len(data))
	if err := binary.Write(w, binary.BigEndian, u16); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
