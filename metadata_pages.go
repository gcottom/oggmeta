package oggmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

const maxCommentPacketSize = 64 << 20

type commentPages struct {
	format      string
	packet      []byte
	firstNumber uint32
	last        []byte
	tailLaces   []byte
	tailPayload []byte
}

// metadataStream reuses a page's backing storage after advancing. Retained
// slices from the last comment page must be consumed before reading audio.
type metadataStream struct {
	source  io.Reader
	buffer  [MaxPageSize]byte
	started bool
	ended   bool
	pending bool
	serial  uint32
	next    uint32
}

func (s *metadataStream) readPage() ([]byte, error) {
	header := s.buffer[:HeaderSize]
	if _, err := io.ReadFull(s.source, header); err != nil {
		return nil, err
	}
	if !bytes.Equal(header[:4], Oggs[:]) || header[4] != 0 || header[5]&^byte(FlagCOP|FlagBOS|FlagEOS) != 0 {
		return nil, errors.New("oggmeta: invalid page header")
	}
	flags := header[5]
	serial := binary.LittleEndian.Uint32(header[14:18])
	sequence := binary.LittleEndian.Uint32(header[18:22])
	if !s.started {
		if flags != FlagBOS {
			return nil, errors.New("oggmeta: missing beginning of stream")
		}
		s.started = true
		s.serial = serial
	} else {
		if s.ended || serial != s.serial || flags&FlagBOS != 0 {
			return nil, errors.New("oggmeta: chained or multiplexed streams cannot be edited")
		}
		if sequence != s.next {
			return nil, errors.New("oggmeta: nonconsecutive page sequence")
		}
	}
	s.next = sequence + 1

	bodyOffset := HeaderSize + int(header[26])
	laces := s.buffer[HeaderSize:bodyOffset]
	if err := readPageBody(s.source, laces); err != nil {
		return nil, err
	}
	payloadSize := 0
	for _, lace := range laces {
		payloadSize += int(lace)
	}
	if len(laces) != 0 {
		if s.pending != (flags&FlagCOP != 0) {
			return nil, errors.New("oggmeta: invalid packet continuation")
		}
		s.pending = laces[len(laces)-1] == MaxSegSize
	}
	s.ended = flags&FlagEOS != 0
	if s.ended && s.pending {
		return nil, io.ErrUnexpectedEOF
	}
	payload := s.buffer[bodyOffset : bodyOffset+payloadSize]
	if err := readPageBody(s.source, payload); err != nil {
		return nil, err
	}
	stored := binary.LittleEndian.Uint32(header[22:26])
	binary.LittleEndian.PutUint32(header[22:26], 0)
	actual := calculateChecksum(header, payload, laces)
	binary.LittleEndian.PutUint32(header[22:26], stored)
	if actual != stored {
		return nil, errors.New("oggmeta: page checksum mismatch")
	}
	return s.buffer[:bodyOffset+payloadSize], nil
}

func readPageBody(r io.Reader, buffer []byte) error {
	_, err := io.ReadFull(r, buffer)
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

func scanCommentPages(s *metadataStream, original io.Writer, replace bool) (commentPages, error) {
	var out commentPages
	id, err := s.readPage()
	if err != nil {
		return out, err
	}
	lacesEnd := HeaderSize + int(id[26])
	laces := id[HeaderSize:lacesEnd]
	for index, lace := range laces {
		if (lace == MaxSegSize) != (index < len(laces)-1) {
			return out, errors.New("oggmeta: identification header does not occupy one page")
		}
	}
	idPacket := id[lacesEnd:]
	var prefix string
	switch {
	case bytes.HasPrefix(idPacket, []byte("\x01vorbis")):
		out.format, prefix = Vorbis, "\x03vorbis"
	case bytes.HasPrefix(idPacket, []byte("OpusHead")):
		out.format, prefix = Opus, "OpusTags"
	default:
		return out, errors.New("oggmeta: unsupported identification header")
	}
	if original != nil {
		if err := writeMetadataBytes(original, id); err != nil {
			return out, err
		}
	}

	out.firstNumber = s.next
	length := 0
	for {
		page, err := s.readPage()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return out, err
		}
		if s.ended || page[26] == 0 {
			return out, errors.New("oggmeta: incomplete comment pages")
		}
		lacesEnd = HeaderSize + int(page[26])
		laces = page[HeaderSize:lacesEnd]
		payload := page[lacesEnd:]
		size, stop := 0, 0
		for index, lace := range laces {
			size += int(lace)
			if lace < MaxSegSize {
				stop = index + 1
				break
			}
		}
		if length == 0 && !bytes.HasPrefix(payload[:size], []byte(prefix)) {
			return out, errors.New("oggmeta: missing codec comment header")
		}
		length += size
		if length > maxCommentPacketSize {
			return out, errors.New("oggmeta: comment packet exceeds 64 MiB")
		}
		if original == nil {
			out.packet = append(out.packet, payload[:size]...)
		} else if !replace {
			if err := writeMetadataBytes(original, page); err != nil {
				return out, err
			}
		}
		if stop != 0 {
			if out.format == Opus && stop != len(laces) {
				return out, errors.New("oggmeta: OpusTags must finish the page")
			}
			out.last, out.tailLaces, out.tailPayload = page, laces[stop:], payload[size:]
			return out, nil
		}
	}
}

func writeMetadataBytes(w io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func writeCommentPages(w io.Writer, old commentPages, packet []byte) (uint32, error) {
	var fullLaces [MaxSegSize]byte
	for i := range fullLaces {
		fullLaces[i] = MaxSegSize
	}
	var header [HeaderSize]byte
	copy(header[:], old.last[:HeaderSize])
	number := old.firstNumber
	header[5] = 0
	granule := uint64(0)
	if old.format == Opus {
		granule = ^uint64(0) // No packet ends on these pages.
	}
	binary.LittleEndian.PutUint64(header[6:14], granule)
	for len(packet) >= MaxSegSequenceSize {
		if err := writeMetadataPage(w, header[:], fullLaces[:], packet[:MaxSegSequenceSize], nil, number); err != nil {
			return 0, err
		}
		packet = packet[MaxSegSequenceSize:]
		number++
		header[5] = FlagCOP
	}

	laceCount := len(packet)/MaxSegSize + 1
	var lastLaces [MaxSegSize]byte
	copy(lastLaces[:laceCount-1], fullLaces[:])
	lastLaces[laceCount-1] = byte(len(packet) % MaxSegSize)
	if laceCount+len(old.tailLaces) > MaxSegSize {
		// The preserved suffix does not fit beside the comment terminator.
		// It begins at a packet boundary and retains its original granule.
		binary.LittleEndian.PutUint64(header[6:14], 0)
		if err := writeMetadataPage(w, header[:], lastLaces[:laceCount], packet, nil, number); err != nil {
			return 0, err
		}
		number++
		header[5] = 0
		laceCount, packet = 0, nil
	}

	copy(lastLaces[laceCount:], old.tailLaces)
	copy(header[6:14], old.last[6:14])
	if err := writeMetadataPage(w, header[:], lastLaces[:laceCount+len(old.tailLaces)], packet, old.tailPayload, number); err != nil {
		return 0, err
	}
	return number - binary.LittleEndian.Uint32(old.last[18:22]), nil
}

func writeMetadataPage(w io.Writer, header, laces, packet, suffix []byte, sequence uint32) error {
	binary.LittleEndian.PutUint32(header[18:22], sequence)
	binary.LittleEndian.PutUint32(header[22:26], 0)
	header[26] = byte(len(laces))
	checksum := checksumParts(header, laces, packet, suffix)
	binary.LittleEndian.PutUint32(header[22:26], checksum)
	for _, part := range [][]byte{header, laces, packet, suffix} {
		if err := writeMetadataBytes(w, part); err != nil {
			return err
		}
	}
	return nil
}

func copyMetadataAudio(w io.Writer, s *metadataStream, pageDelta uint32) error {
	for {
		page, err := s.readPage()
		if err == io.EOF {
			if !s.ended {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err != nil {
			return err
		}
		if pageDelta != 0 {
			sequence := binary.LittleEndian.Uint32(page[18:22])
			binary.LittleEndian.PutUint32(page[18:22], sequence+pageDelta)
			binary.LittleEndian.PutUint32(page[22:26], 0)
			binary.LittleEndian.PutUint32(page[22:26], checksumParts(page))
		}
		if err := writeMetadataBytes(w, page); err != nil {
			return err
		}
	}
}
