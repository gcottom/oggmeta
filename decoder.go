package oggmeta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"reflect"
	"strings"
)

func (dec *OGGDecoder) Decode() (*OGGPage, error) {
	page, _, err := dec.decodePage()
	return page, err
}

func (dec *OGGDecoder) decodePage() (*OGGPage, []byte, error) {
	if dec.Reader == nil {
		return nil, nil, errors.New("oggmeta: nil source")
	}
	oggPage := new(OGGPage)

	if err := binary.Read(dec.Reader, binary.LittleEndian, &oggPage.Header); err != nil {
		return nil, nil, err
	}

	if oggPage.Header.Oggs != Oggs {
		return nil, nil, new(ErrInvalidOggs)
	}

	if oggPage.Header.Version != 0 {
		return nil, nil, errors.New("oggmeta: unsupported page version")
	}
	if oggPage.Header.Segments < 1 {
		return nil, nil, new(ErrBadSegs)
	}

	segmentTable := make([]byte, oggPage.Header.Segments)

	if _, err := io.ReadFull(dec.Reader, segmentTable); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, nil, err
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
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, nil, err
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
		return nil, nil, errors.New("oggmeta: page checksum mismatch")
	}

	oggPage.Header.CRC = oldCRC
	return oggPage, segmentTable, nil
}

func (dec *OGGDecoder) ReadTags() (*OggTag, error) {
	for {
		oggPage, segments, err := dec.decodePage()
		if err != nil {
			if err == io.EOF {
				return nil, errors.New("oggmeta: no Vorbis or Opus comments found")
			}
			return nil, err
		}

		for i, packet := range oggPage.Packets {
			if i == 0 && oggPage.Header.Flags&FlagCOP != 0 {
				continue
			}
			if (bytes.HasPrefix(packet, VorbisPrefix) || bytes.HasPrefix(packet, OpusPrefix)) && i == len(oggPage.Packets)-1 && segments[len(segments)-1] == MaxSegSize {
				return nil, errors.New("oggmeta: comments spanning pages are unsupported")
			}
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
		key, fieldValue, ok := strings.Cut(comment, "=")
		if !ok || key == "" {
			continue
		}
		fieldName := strings.ToUpper(key)
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

			continue
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
	if dataLen < 0 || dataLen > reader.Len() {
		return nil, fmt.Errorf("oggmeta: invalid picture length %d", dataLen)
	}
	output := make([]byte, dataLen)
	if _, err = io.ReadFull(reader, output); err != nil {
		return nil, err
	}

	return output, nil
}
