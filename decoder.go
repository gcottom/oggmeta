package ogg

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"reflect"
	"strings"
)

func (dec *OGGDecoder) Decode() (*OGGPage, error) {
	oggPage := new(OGGPage)

	if err := binary.Read(dec.Reader, binary.LittleEndian, &oggPage.Header); err != nil {
		return nil, err
	}

	if oggPage.Header.Oggs != Oggs {
		return nil, new(ErrInvalidOggs)
	}

	if oggPage.Header.Segments < 1 {
		return nil, new(ErrBadSegs)
	}

	segmentTable := make([]byte, oggPage.Header.Segments)

	if _, err := io.ReadFull(dec.Reader, segmentTable); err != nil {
		return nil, err
	}

	packetLengths := make([]int, 0)
	payloadLength := 0
	packetContinues := false

	for _, segmentLength := range segmentTable {
		if packetContinues {
			packetLengths[len(packetLengths)-1] += int(segmentLength)
		} else {
			packetLengths = append(packetLengths, int(segmentLength))
		}

		packetContinues = segmentLength == MaxSegSize
		payloadLength += int(segmentLength)
	}

	payloadBytes := make([]byte, 0)

	for _, packetLength := range packetLengths {
		packet := make([]byte, packetLength)

		if _, err := io.ReadFull(dec.Reader, packet); err != nil {
			return nil, err
		}

		oggPage.Packets = append(oggPage.Packets, packet)
		payloadBytes = append(payloadBytes, packet...)
	}

	oldCRC := oggPage.Header.CRC
	oggPage.Header.CRC = 0

	headerBytes := oggPage.Header.toBytesSlice()
	segTableBytes := segmentTable

	crc32 := calculateChecksum(headerBytes, payloadBytes, segTableBytes)
	if crc32 != oldCRC {
		fmt.Println("CRC32 mismatch")
	}

	return oggPage, nil
}

func (dec *OGGDecoder) ReadTags() (*OggTag, error) {
	for {
		oggPage, err := dec.Decode()
		if err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, err
		}

		for _, packet := range oggPage.Packets {
			switch {
			case bytes.HasPrefix(packet, VorbisPrefix):
				dec.TagReader = bytes.NewReader(packet)
				io.ReadFull(dec.TagReader, make([]byte, len(VorbisPrefix)))
				resultTag, err := dec.readComments()
				if err != nil {
					return nil, err
				}
				resultTag.reader = dec.Reader
				resultTag.Codec = Vorbis
				return resultTag, nil

			case bytes.HasPrefix(packet, OpusPrefix):
				dec.TagReader = bytes.NewReader(packet)
				io.ReadFull(dec.TagReader, make([]byte, len(OpusPrefix)))
				resultTag, err := dec.readComments()
				if err != nil {
					return nil, err
				}
				resultTag.reader = dec.Reader
				resultTag.Codec = Opus
				return resultTag, nil
			}
		}
	}
}

func (dec *OGGDecoder) readComments() (*OggTag, error) {
	oggTag := new(OggTag)
	vendorLength, err := readUint32(dec.TagReader)
	if err != nil {
		return nil, err
	}

	oggTag.Vendor, err = readString(dec.TagReader, uint(vendorLength))
	if err != nil {
		return nil, err
	}

	commentsLength, err := readUint32(dec.TagReader)
	if err != nil {
		return nil, err
	}

	for i := uint32(0); i < commentsLength; i++ {
		commentLength, err := readUint32(dec.TagReader)
		if err != nil {
			return nil, err
		}
		comment, err := readString(dec.TagReader, uint(commentLength))
		if err != nil {
			return nil, err
		}
		splitComment := strings.Split(comment, "=")
		if len(splitComment) != 2 {
			continue
		}
		fieldName := strings.ToUpper(splitComment[0])
		fieldValue := splitComment[1]
		if fieldName == "METADATA_BLOCK_PICTURE" {
			// process picture block
			data, err := base64.StdEncoding.DecodeString(fieldValue)
			if err != nil {
				return nil, err
			}
			data, err = dec.readPictureBlock(data)
			if err != nil {
				return nil, err
			}
			if len(data) > 0 {
				if img, _, err := image.Decode(bytes.NewReader(data)); err != nil {
					return nil, err
				} else {
					oggTag.CoverArt = &img
				}
			}

		}
		if tagField, ok := tagFieldMapping[fieldName]; ok {
			field := reflect.ValueOf(oggTag).Elem().FieldByName(tagField)
			field.SetString(fieldValue)
		} else {
			if oggTag.UnmappedFields == nil {
				oggTag.UnmappedFields = make(map[string]string)
			}
			oggTag.UnmappedFields[fieldName] = fieldValue
		}
	}
	return oggTag, nil
}

func (dec *OGGDecoder) readPictureBlock(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	//skipping picture type
	if _, err := readInt(reader, 4); err != nil {
		return nil, err
	}
	mimeLen, err := readUint(reader, 4)
	if err != nil {
		return nil, err
	}
	//skipping mime type
	if _, err := readString(reader, mimeLen); err != nil {
		return nil, err
	}
	descLen, err := readUint(reader, 4)
	if err != nil {
		return nil, err
	}
	//skipping description
	if _, err := readString(reader, descLen); err != nil {
		return nil, err
	}

	//skip width <32>, height <32>, colorDepth <32>, coloresUsed <32>

	// width
	if _, err = readInt(reader, 4); err != nil {
		return nil, err
	}
	// height
	if _, err = readInt(reader, 4); err != nil {
		return nil, err
	}
	// color depth
	if _, err = readInt(reader, 4); err != nil {
		return nil, err
	}
	// colors used
	if _, err = readInt(reader, 4); err != nil {
		return nil, err
	}

	dataLen, err := readInt(reader, 4)
	if err != nil {
		return nil, err
	}
	output := make([]byte, dataLen)
	if _, err = io.ReadFull(reader, output); err != nil {
		return nil, err
	}

	return output, nil
}
