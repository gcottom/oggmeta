package oggmeta

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
	return checksumParts(header, segmentTable, segments)
}

func checksumParts(parts ...[]byte) uint32 {
	var crc uint32
	for _, part := range parts {
		for _, b := range part {
			crc = (crc << 8) ^ crcTable[(crc>>24)^uint32(b)]
		}
	}
	return crc
}
