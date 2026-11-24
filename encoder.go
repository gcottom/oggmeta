package oggmeta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/aler9/writerseeker"
)

func (enc *OGGEncoder) WritePackets(flag byte, granulePosition int64, packets [][]byte) error {
	if len(packets) == 0 {
		packets = dummyPacket[:]
	}
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

// SaveTags saves edited comments into writer. The replacement must fit on the original comment page.
func SaveTags(tag *OggTag, writer io.Writer) error {
	if tag == nil || tag.reader == nil || writer == nil {
		return errors.New("oggmeta: source, destination and tags are required")
	}
	if tag.Codec != Vorbis && tag.Codec != Opus {
		return errors.New("oggmeta: unsupported codec")
	}
	if _, err := tag.reader.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tempWriter := &writerseeker.WriterSeeker{}
	defer tempWriter.Close()
	decoder := &OGGDecoder{Reader: tag.reader}
	fields := make(map[string]string)
	for k, v := range tag.UnmappedFields {
		key := strings.ToUpper(k)
		if _, mapped := tagFieldMapping[key]; !mapped && key != "METADATA_BLOCK_PICTURE" && key != "" && !strings.Contains(key, "=") {
			fields[key] = v
		}
	}
	for key, field := range tagFieldMapping {
		if val := reflect.ValueOf(tag).Elem().FieldByName(field).String(); val != "" {
			fields[key] = val
		}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	comments := make([]string, 0, len(keys))
	for _, key := range keys {
		comments = append(comments, key+"="+fields[key])
	}
	var img []byte
	if tag.CoverArt != nil {
		if *tag.CoverArt == nil {
			return errors.New("oggmeta: nil cover image")
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, *tag.CoverArt, nil); err != nil {
			return err
		}
		var err error
		img, err = createMetadataBlockPicture(buf.Bytes())
		if err != nil {
			return err
		}
	}
	replacement := createCommentPacket(comments, img, tag.Codec, tag.Vendor)
	var serial uint32
	var sawPage, replaced, ended bool
	for {
		page, segs, err := decoder.decodePage()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !sawPage {
			serial = page.Header.SerialNumber
			sawPage = true
		} else if page.Header.SerialNumber != serial || ended || page.Header.Flags&FlagBOS != 0 {
			return errors.New("oggmeta: chained or multiplexed Ogg is not supported for saving")
		}
		ended = page.Header.Flags&FlagEOS != 0
		// Only the codec's comment packet is replaced; keep original lacing and page numbering elsewhere.
		packetStart := 0
		for i, packet := range page.Packets {
			packetEnd := packetStart
			for packetEnd < len(segs) {
				v := segs[packetEnd]
				packetEnd++
				if v < MaxSegSize {
					break
				}
			}
			if !replaced && !(i == 0 && page.Header.Flags&FlagCOP != 0) &&
				((tag.Codec == Vorbis && bytes.HasPrefix(packet, VorbisPrefix)) || (tag.Codec == Opus && bytes.HasPrefix(packet, OpusPrefix))) {
				if segs[packetEnd-1] == MaxSegSize {
					return errors.New("oggmeta: comments spanning pages are unsupported")
				}
				n := len(replacement) / MaxSegSize
				if len(segs)-(packetEnd-packetStart)+n+1 > MaxSegSize {
					return errors.New("oggmeta: updated comments do not fit their original page")
				}
				newSegs := append([]byte(nil), segs[:packetStart]...)
				for j := 0; j < n; j++ {
					newSegs = append(newSegs, MaxSegSize)
				}
				newSegs = append(newSegs, byte(len(replacement)%MaxSegSize))
				newSegs = append(newSegs, segs[packetEnd:]...)
				segs = newSegs
				page.Packets[i] = replacement
				replaced = true
				break
			}
			packetStart = packetEnd
		}
		payload := bytes.Join(page.Packets, nil)
		page.Header.Segments = byte(len(segs))
		page.Header.CRC = 0
		page.Header.CRC = calculateChecksum(page.Header.toBytesSlice(), payload, segs)
		for _, data := range [][]byte{page.Header.toBytesSlice(), segs, payload} {
			if _, err := tempWriter.Write(data); err != nil {
				return err
			}
		}
	}
	if !replaced {
		return errors.New("oggmeta: no matching comment packet found")
	}
	if !ended {
		return io.ErrUnexpectedEOF
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
	if _, err := io.Copy(writer, tempWriter.BytesReader()); err != nil {
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

func createCommentPacket(commentFields []string, albumArt []byte, codec, vendorString string) []byte {
	if vendorString == "" {
		vendorString = "gcottom-oggmeta"
	}

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
