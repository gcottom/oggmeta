package oggmeta

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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
	tempWriter, err := os.CreateTemp("", "oggmeta-*")
	if err != nil {
		return err
	}
	defer discardMetadataStage(tempWriter)
	decoder := &OGGDecoder{Reader: tag.reader}
	fields := make(map[string]string)
	inputKeys := make([]string, 0, len(tag.UnmappedFields))
	for k := range tag.UnmappedFields {
		inputKeys = append(inputKeys, k)
	}
	sort.Strings(inputKeys)
	for _, k := range inputKeys {
		key := strings.ToUpper(k)
		if _, mapped := tagFieldMapping[key]; !mapped && key != "METADATA_BLOCK_PICTURE" && key != "" && !strings.Contains(key, "=") {
			fields[key] = tag.UnmappedFields[k]
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
	if tag.CoverArt != nil && *tag.CoverArt == nil {
		return errors.New("oggmeta: nil cover image")
	}
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
				replacement, err := replaceLegacyComments(packet, tag, fields, keys)
				if err != nil {
					return err
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
			if err := writeMetadataBytes(tempWriter, data); err != nil {
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
		copyErr := copyMetadataStage(w2, tempWriter)
		closeErr := w2.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err = (writer.(*os.File)).Seek(0, io.SeekEnd); err != nil {
			return err
		}
		return nil
	}
	return copyMetadataStage(writer, tempWriter)
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

func replaceLegacyComments(packet []byte, tag *OggTag, fields map[string]string, keys []string) ([]byte, error) {
	prefix := commentPrefix(tag.Codec)
	if !bytes.HasPrefix(packet, []byte(prefix)) {
		return nil, errors.New("oggmeta: invalid codec comment packet")
	}
	// Legacy reads historically accepted a Vorbis packet without its framing
	// bit. Keep that read contract and insert the bit when saving such a packet.
	original, err := parseCommentMetadata(packet[len(prefix):], "")
	if err != nil {
		return nil, err
	}
	preserveArtwork := !tag.clearAll && legacyPictureMatches(tag.CoverArt, original.entries)
	lastValues := make(map[string]string)
	for _, entry := range original.entries {
		if key, value, ok := strings.Cut(entry, "="); ok && key != "" {
			lastValues[strings.ToUpper(key)] = value
		}
	}
	seen := make(map[string]bool)
	entries := make([]string, 0, len(original.entries)+len(fields))
	for _, entry := range original.entries {
		key, _, ok := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if !ok || key == "" {
			if !tag.clearAll {
				entries = append(entries, entry)
			}
			continue
		}
		if key == pictureComment {
			if preserveArtwork {
				entries = append(entries, entry)
			}
			continue
		}
		value, exists := fields[key]
		_, mapped := tagFieldMapping[key]
		preserve := (exists && value == lastValues[key]) ||
			(!exists && !tag.clearAll && mapped && lastValues[key] == "") ||
			key == "WAVEFORMATEXTENSIBLE_CHANNEL_MASK"
		if preserve {
			entries = append(entries, entry)
		} else if exists && !seen[key] {
			entries = append(entries, key+"="+value)
		}
		seen[key] = true
	}
	for _, key := range keys {
		if !seen[key] {
			entries = append(entries, key+"="+fields[key])
		}
	}
	if !preserveArtwork && tag.CoverArt != nil {
		var buffer bytes.Buffer
		if err := jpeg.Encode(&buffer, *tag.CoverArt, nil); err != nil {
			return nil, err
		}
		artwork, err := createMetadataBlockPicture(buffer.Bytes())
		if err != nil {
			return nil, err
		}
		entries = append(entries, pictureComment+"="+base64.StdEncoding.EncodeToString(artwork))
	}
	vendor := tag.Vendor
	if vendor == "" && original.vendor != "" {
		vendor = "gcottom-oggmeta"
	}
	if tag.Codec == Vorbis && (len(original.trailer) == 0 || original.trailer[0]&1 == 0) {
		original.trailer = []byte{1}
	}
	return encodeCommentMetadata(prefix, vendor, entries, original.trailer)
}

func legacyPictureMatches(current *image.Image, entries []string) bool {
	decoder := &OGGDecoder{}
	// The legacy read path exposes the last decoded nonempty image.
	for i := len(entries) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(entries[i], "=")
		if !ok || !strings.EqualFold(key, pictureComment) {
			continue
		}
		block, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return false
		}
		data, err := decoder.readPictureBlock(block)
		if err != nil {
			return false
		}
		if len(data) == 0 {
			continue
		}
		if current == nil {
			return false
		}
		original, _, err := image.Decode(bytes.NewReader(data))
		return err == nil && equalLegacyImages(original, *current)
	}
	return current == nil
}

func equalLegacyImages(a, b image.Image) bool {
	bounds := a.Bounds()
	if bounds != b.Bounds() {
		return false
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}
