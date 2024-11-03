package oggmeta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/aler9/writerseeker"
)

func (enc *OGGEncoder) WritePackets(flag byte, granulePosition int64, packets [][]byte) error {
	header := OGGPageHeader{
		Oggs:               Oggs,
		Version:            0,
		Flags:              flag,
		GranulePosition:    granulePosition,
		SerialNumber:       enc.Serial,
		PageSequenceNumber: enc.PageNumber,
	}

	segtbl, good, bad := enc.Segmentize(segmentizePayload{packets[0], packets[1:], nil})
	if err := enc.writePage(&header, segtbl, good); err != nil {
		return err
	}

	header.Flags |= FlagCOP
	for len(bad.leftPay) > 0 {
		segtbl, good, bad = enc.Segmentize(bad)
		if err := enc.writePage(&header, segtbl, good); err != nil {
			return err
		}
	}

	return nil
}

func (enc *OGGEncoder) Segmentize(pay segmentizePayload) ([]byte, segmentizePayload, segmentizePayload) {
	segtbl := make([]byte, MaxSegSize)
	i := 0

	s255s := len(pay.leftPay) / MaxSegSize
	rem := len(pay.leftPay) % MaxSegSize
	for i < len(segtbl) && s255s > 0 {
		segtbl[i] = MaxSegSize
		i++
		s255s--
	}
	if i < MaxSegSize {
		segtbl[i] = byte(rem)
		i++
	} else {
		leftStart := len(pay.leftPay) - (s255s * MaxSegSize) - rem
		good := segmentizePayload{pay.leftPay[0:leftStart], nil, nil}
		bad := segmentizePayload{pay.leftPay[leftStart:], pay.middlePay, nil}
		return segtbl, good, bad
	}

	// Now loop through the rest and track if we need to split
	for p := 0; p < len(pay.middlePay); p++ {
		s255s := len(pay.middlePay[p]) / MaxSegSize
		rem := len(pay.middlePay[p]) % MaxSegSize
		for i < len(segtbl) && s255s > 0 {
			segtbl[i] = MaxSegSize
			i++
			s255s--
		}
		if i < MaxSegSize {
			segtbl[i] = byte(rem)
			i++
		} else {
			right := len(pay.middlePay[p]) - (s255s * MaxSegSize) - rem
			good := segmentizePayload{pay.leftPay, pay.middlePay[0:p], pay.middlePay[p][0:right]}
			bad := segmentizePayload{pay.middlePay[p][right:], pay.middlePay[p+1:], nil}
			return segtbl, good, bad
		}
	}

	good := pay
	bad := segmentizePayload{}
	return segtbl[0:i], good, bad
}

// EncodeBOS writes a beginning-of-stream packet to the ogg stream with a given granule position.
// Large packets are split across multiple pages with continuation-of-packet flag set.
// Packets can be empty or nil, resulting in a single segment of size 0.
func (enc *OGGEncoder) EncodeBOS(granule int64, packets [][]byte) error {
	if len(packets) == 0 {
		packets = dummyPacket[:]
	}
	return enc.WritePackets(FlagBOS, granule, packets)
}

// Encode writes a data packet to the ogg stream with a given granule position.
// Large packets are split across multiple pages with continuation-of-packet flag set.
// Packets can be empty or nil, resulting in a single segment of size 0.
func (enc *OGGEncoder) Encode(granule int64, packets [][]byte) error {
	if len(packets) == 0 {
		packets = dummyPacket[:]
	}
	return enc.WritePackets(0, granule, packets)
}

// EncodeEOS writes a end-of-stream packet to the ogg stream.
// Packets can be empty or nil, resulting in a single segment of size 0.
func (enc *OGGEncoder) EncodeEOS(granule int64, packets [][]byte) error {
	if len(packets) == 0 {
		packets = dummyPacket[:]
	}
	return enc.WritePackets(FlagEOS, granule, packets)
}

func (enc *OGGEncoder) writePage(h *OGGPageHeader, segtbl []byte, pay segmentizePayload) error {
	page := &OGGPage{}
	h.PageSequenceNumber = enc.PageNumber
	enc.PageNumber++
	h.Segments = byte(len(segtbl))
	payBuffer := new(bytes.Buffer)
	//_ = binary.Write(hb, byteOrder, h)

	payBuffer.Write(pay.leftPay)
	for _, p := range pay.middlePay {
		payBuffer.Write(p)
	}
	payBuffer.Write(pay.rightPay)

	page.Header = *h

	headerBytes := page.Header.toBytesSlice()
	segTableBytes := segtbl
	payloadBytes := payBuffer.Bytes()

	crc32 := calculateChecksum(headerBytes, payloadBytes, segTableBytes)
	page.Header.CRC = crc32

	headerBytes = page.Header.toBytesSlice()

	if _, err := enc.Writer.Write(headerBytes); err != nil {
		return err
	}
	if _, err := enc.Writer.Write(segTableBytes); err != nil {
		return err
	}
	if _, err := enc.Writer.Write(payloadBytes); err != nil {
		return err
	}
	return nil
}

func SaveTags(tag *OggTag, writer io.Writer) error {
	if _, err := tag.reader.Seek(0, 0); err != nil {
		return err
	}
	tempWriter := &writerseeker.WriterSeeker{}
	decoder := &OGGDecoder{Reader: tag.reader}
	encoder := &OGGEncoder{Writer: tempWriter}

	page, err := decoder.Decode()
	if err != nil {
		return err
	}

	if err = encoder.EncodeBOS(page.Header.GranulePosition, page.Packets); err != nil {
		return err
	}

	for {
		page, err = decoder.Decode()
		if err != nil {
			if err == io.EOF {
				break // Reached the end of the input Ogg stream
			}
			return err
		}

		if bytes.HasPrefix(page.Packets[0], VorbisPrefix) || bytes.HasPrefix(page.Packets[0], OpusPrefix) {
			commentFields := make([]string, 0)
			for key, value := range tagFieldMapping {
				if reflect.ValueOf(tag).Elem().FieldByName(value).IsValid() {
					commentFields = append(commentFields, fmt.Sprintf("%s=%s", key, reflect.ValueOf(tag).Elem().FieldByName(value).String()))
				}
			}
			img := make([]byte, 0)
			if tag.CoverArt != nil {
				// Convert album art image to JPEG format
				buf := new(bytes.Buffer)
				if err = jpeg.Encode(buf, *tag.CoverArt, nil); err == nil {
					img, _ = createMetadataBlockPicture(buf.Bytes())
				}
			}
			page.Packets[0] = createCommentPacket(commentFields, img, tag.Codec)
			if err = encoder.Encode(page.Header.GranulePosition, page.Packets); err != nil {
				return err
			}
		} else {
			if page.Header.Flags == FlagEOS {
				if err = encoder.EncodeEOS(page.Header.GranulePosition, page.Packets); err != nil {
					return err
				}
			} else {
				if err = encoder.Encode(page.Header.GranulePosition, page.Packets); err != nil {
					return err
				}
			}
		}
	}
	if reflect.TypeOf(writer) == reflect.TypeOf(new(os.File)) {
		path, err := filepath.Abs((writer.(*os.File)).Name())
		if err != nil {
			return err
		}
		w2, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		defer w2.Close()
		if _, err = w2.Write(tempWriter.Bytes()); err != nil {
			return err
		}
		if _, err = (writer.(*os.File)).Seek(0, io.SeekEnd); err != nil {
			return err
		}
		return nil
	}
	if _, err := io.Copy(writer, bytes.NewReader(tempWriter.Bytes())); err != nil {
		return err
	}
	return nil
}

func createMetadataBlockPicture(albumArtData []byte) ([]byte, error) {
	mimeType := "image/jpeg"
	description := "Cover"
	img, _, err := image.DecodeConfig(bytes.NewReader(albumArtData))
	if err != nil {
		return nil, errors.New("failed to decode album art image")
	}
	res := bytes.NewBuffer([]byte{})
	res.Write(encodeUint32(uint32(3)))
	res.Write(encodeUint32(uint32(len(mimeType))))
	res.Write([]byte(mimeType))
	res.Write(encodeUint32(uint32(len(description))))
	res.Write([]byte(description))
	res.Write(encodeUint32(uint32(img.Width)))
	res.Write(encodeUint32(uint32(img.Height)))
	res.Write(encodeUint32(24))
	res.Write(encodeUint32(0))
	res.Write(encodeUint32(uint32(len(albumArtData))))
	res.Write(albumArtData)
	return res.Bytes(), nil
}

func createCommentPacket(commentFields []string, albumArt []byte, codec string) []byte {
	vendorString := "gcottom-oggmeta"

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(vendorString)))
	commentPacket := append(buf, []byte(vendorString)...)
	if len(albumArt) > 0 {
		binary.LittleEndian.PutUint32(buf, uint32(len(commentFields)+1))
	} else {
		binary.LittleEndian.PutUint32(buf, uint32(len(commentFields)))
	}
	commentPacket = append(commentPacket, buf...)

	for _, field := range commentFields {
		binary.LittleEndian.PutUint32(buf, uint32(len(field)))
		commentPacket = append(commentPacket, buf...)
		commentPacket = append(commentPacket, []byte(field)...)
	}
	if codec == Vorbis {
		commentPacket = append([]byte("\x03vorbis"), commentPacket...)
	} else {
		commentPacket = append([]byte("OpusTags"), commentPacket...)
	}
	if len(albumArt) > 1 {
		albumArtBase64 := base64.StdEncoding.EncodeToString(albumArt)
		fieldLength := len("METADATA_BLOCK_PICTURE=") + len(albumArtBase64)
		binary.LittleEndian.PutUint32(buf, uint32(fieldLength))
		commentPacket = append(commentPacket, buf...)
		commentPacket = append(commentPacket, []byte("METADATA_BLOCK_PICTURE=")...)
		commentPacket = append(commentPacket, []byte(albumArtBase64)...)
	}
	if codec == Vorbis {
		commentPacket = append(commentPacket, []byte("\x01")...)
	}

	return commentPacket
}
