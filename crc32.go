package ogg

var crcTable [256]uint32

func init() {
	for i := range crcTable {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		crcTable[i] = r
	}
}

func calculateChecksum(header, segments, segmentTable []byte) uint32 {
	data := append(header, segmentTable...)
	data = append(data, segments...)

	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ crcTable[(crc>>24)^uint32(b)]
	}
	return crc
}
