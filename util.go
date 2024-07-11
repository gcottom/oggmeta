package ogg

import (
	"bytes"
	"encoding/binary"
	"io"
)

func readUint32(reader io.Reader) (uint32, error) {
	data, err := readBytes(reader, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data), nil
}

func readBytes(reader io.Reader, n uint) ([]byte, error) {
	data := make([]byte, n)
	_, err := io.ReadFull(reader, data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func readString(reader io.Reader, n uint) (string, error) {
	data, err := readBytes(reader, n)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func getInt(b []byte) int {
	var n int
	for _, x := range b {
		n = n << 8
		n |= int(x)
	}
	return n
}
func readInt(r io.Reader, n uint) (int, error) {
	b, err := readBytes(r, n)
	if err != nil {
		return 0, err
	}
	return getInt(b), nil
}

func readUint(r io.Reader, n uint) (uint, error) {
	x, err := readInt(r, n)
	if err != nil {
		return 0, err
	}
	return uint(x), nil
}

func encodeUint32(n uint32) []byte {
	buf := bytes.NewBuffer([]byte{})
	if err := binary.Write(buf, binary.BigEndian, n); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (o OGGPageHeader) toBytesSlice() []byte {
	byteOrder := binary.LittleEndian
	b := new(bytes.Buffer)
	_ = binary.Write(b, byteOrder, o.Oggs)
	_ = binary.Write(b, byteOrder, o.Version)
	_ = binary.Write(b, byteOrder, o.Flags)
	_ = binary.Write(b, byteOrder, o.GranulePosition)
	_ = binary.Write(b, byteOrder, o.SerialNumber)
	_ = binary.Write(b, byteOrder, o.PageSequenceNumber)
	_ = binary.Write(b, byteOrder, o.CRC)
	_ = binary.Write(b, byteOrder, o.Segments)
	return b.Bytes()
}
