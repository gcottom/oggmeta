package oggmeta

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gcottom/audiometa-core/v2"
)

const (
	pictureComment    = "METADATA_BLOCK_PICTURE"
	maxCommentEntries = 1 << 20
)

type commentMetadata struct {
	vendor  string
	entries []string
	trailer []byte
}

// OpenMetadata opens the canonical metadata editor for a single Vorbis or
// Opus logical stream. The editor borrows source, seeks it to the beginning
// for each save, and does not close it. The source must remain unchanged for
// the lifetime of the editor. Concurrent use of the editor or source is
// unsupported.
func OpenMetadata(source io.ReadSeeker) (*core.Editor, error) {
	if source == nil {
		return nil, errors.New("oggmeta: nil source")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	pages, err := scanCommentPages(&metadataStream{source: source}, nil, false)
	if err != nil {
		return nil, err
	}
	codec := pages.format
	prefix := commentPrefix(codec)
	metadata, err := parseCommentMetadata(pages.packet[len(prefix):], codec)
	if err != nil {
		return nil, err
	}
	format := core.Vorbis
	if codec == Opus {
		format = core.Opus
	}
	textEntries := make([]string, 0, len(metadata.entries))
	pictureIndexes := make(map[int]bool)
	var pictures, fallbackPictures []core.Value
	for i, entry := range metadata.entries {
		key, value, hasValue := strings.Cut(entry, "=")
		if hasValue && strings.EqualFold(key, pictureComment) {
			picture, err := parsePictureComment(value)
			if err == nil {
				projected := core.PictureValue(picture)
				if core.Validate(core.Artwork, projected) == nil {
					pictures = append(pictures, projected)
					pictureIndexes[i] = true
				}
			}
			continue
		}
		if hasValue && strings.EqualFold(key, core.CommentFallbackName(core.Artwork)) {
			if v, err := core.Decode(core.Artwork, value); err == nil {
				fallbackPictures = append(fallbackPictures, v)
			}
		}
		textEntries = append(textEntries, entry)
	}
	fields, custom := core.ReadComments(format, textEntries)
	if len(pictures) != 0 {
		fields[core.Artwork] = pictures
	} else if len(fallbackPictures) != 0 {
		fields[core.Artwork] = fallbackPictures
	}
	return core.NewEditor(fields, custom, func(w io.Writer, changes core.Changes) error {
		if changes.Empty() {
			return saveMetadataStream(source, w, codec, nil)
		}
		entries, err := editCommentMetadata(format, metadata.entries, pictureIndexes, changes)
		if err != nil {
			return err
		}
		trailer := metadata.trailer
		if changes.ClearUnknown && format == core.Opus && len(trailer) != 0 && trailer[0]&1 == 0 {
			trailer = nil
		}
		var replacement []byte
		if !sameEntries(entries, metadata.entries) || len(trailer) != len(metadata.trailer) {
			replacement, err = encodeCommentMetadata(prefix, metadata.vendor, entries, trailer)
			if err != nil {
				return err
			}
		}
		return saveMetadataStream(source, w, codec, replacement)
	}), nil
}

func commentPrefix(format string) string {
	if format == Opus {
		return "OpusTags"
	}
	return "\x03vorbis"
}

func parseCommentMetadata(packet []byte, format string) (commentMetadata, error) {
	var metadata commentMetadata
	vendor, rest, err := readCommentBytes(packet)
	if err != nil || len(rest) < 4 {
		return metadata, io.ErrUnexpectedEOF
	}
	metadata.vendor = string(vendor)
	count := binary.LittleEndian.Uint32(rest[:4])
	rest = rest[4:]
	if uint64(count) > uint64(len(rest)/4) {
		return metadata, io.ErrUnexpectedEOF
	}
	if count > maxCommentEntries {
		return metadata, errors.New("oggmeta: too many comment entries")
	}
	metadata.entries = make([]string, 0, int(count))
	for i := uint32(0); i < count; i++ {
		var entry []byte
		entry, rest, err = readCommentBytes(rest)
		if err != nil {
			return metadata, err
		}
		metadata.entries = append(metadata.entries, string(entry))
	}
	if format == Vorbis && (len(rest) == 0 || rest[0]&1 == 0) {
		return metadata, errors.New("oggmeta: missing Vorbis comment framing bit")
	}
	metadata.trailer = append([]byte(nil), rest...)
	return metadata, nil
}

func readCommentBytes(data []byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	size := binary.LittleEndian.Uint32(data[:4])
	if uint64(size) > uint64(len(data)-4) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	end := 4 + int(size)
	return data[4:end], data[end:], nil
}

func editCommentMetadata(format core.Format, entries []string, pictureIndexes map[int]bool, changes core.Changes) ([]string, error) {
	newPictures, editPictures := changes.Fields[core.Artwork]
	if editPictures || changes.ClearUnknown {
		edited := make([]string, 0, len(entries))
		for i, entry := range entries {
			key, value, _ := strings.Cut(entry, "=")
			if (editPictures && pictureIndexes[i]) || (changes.ClearUnknown && strings.EqualFold(key, pictureComment)) {
				continue
			}
			if editPictures && len(newPictures) == 0 && strings.EqualFold(key, core.CommentFallbackName(core.Artwork)) {
				if _, err := core.Decode(core.Artwork, value); err == nil {
					continue
				}
			}
			edited = append(edited, entry)
		}
		entries = edited
	}
	entries, err := core.EditComments(format, entries, changes)
	if err != nil {
		return nil, err
	}
	var icons [3]bool
	for _, value := range newPictures {
		picture, ok := value.Picture()
		if !ok {
			return nil, errors.New("oggmeta: invalid artwork value")
		}
		if picture.Role == 1 || picture.Role == 2 {
			if icons[picture.Role] {
				return nil, fmt.Errorf("oggmeta: duplicate artwork role %d", picture.Role)
			}
			icons[picture.Role] = true
		}
		if len(picture.Data) > (maxCommentPacketSize/4)*3 {
			return nil, errors.New("oggmeta: artwork exceeds comment packet limit")
		}
		block, err := core.MarshalPictureBlock(picture)
		if err != nil {
			return nil, err
		}
		entries = append(entries, pictureComment+"="+base64.StdEncoding.EncodeToString(block))
	}
	return entries, nil
}

func parsePictureComment(value string) (core.Picture, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return core.Picture{}, err
	}
	return core.ParsePictureBlock(data)
}

func sameEntries(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, entry := range a {
		if entry != b[i] {
			return false
		}
	}
	return true
}

func encodeCommentMetadata(prefix, vendor string, entries []string, trailer []byte) ([]byte, error) {
	if len(entries) > maxCommentEntries {
		return nil, errors.New("oggmeta: too many comment entries")
	}
	size := uint64(len(prefix)) + uint64(len(vendor)) + uint64(len(trailer)) + 8
	for _, entry := range entries {
		size += uint64(len(entry)) + 4
		if size > maxCommentPacketSize {
			return nil, errors.New("oggmeta: comment packet exceeds 64 MiB")
		}
	}
	if size > maxCommentPacketSize {
		return nil, errors.New("oggmeta: comment packet exceeds 64 MiB")
	}
	packet := make([]byte, int(size))
	offset := copy(packet, prefix)
	binary.LittleEndian.PutUint32(packet[offset:], uint32(len(vendor)))
	offset += 4 + copy(packet[offset+4:], vendor)
	binary.LittleEndian.PutUint32(packet[offset:], uint32(len(entries)))
	offset += 4
	for _, entry := range entries {
		binary.LittleEndian.PutUint32(packet[offset:], uint32(len(entry)))
		offset += 4 + copy(packet[offset+4:], entry)
	}
	copy(packet[offset:], trailer)
	return packet, nil
}

func saveMetadataStream(source io.ReadSeeker, destination io.Writer, format string, packet []byte) error {
	if destination == nil {
		return errors.New("oggmeta: nil destination")
	}
	if sourceFile, ok := source.(*os.File); ok {
		if destinationFile, ok := destination.(*os.File); ok {
			from, err := sourceFile.Stat()
			if err != nil {
				return err
			}
			to, err := destinationFile.Stat()
			if err != nil {
				return err
			}
			if os.SameFile(from, to) {
				return errors.New("oggmeta: source and destination must be distinct")
			}
		}
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	temp, err := os.CreateTemp("", "oggmeta-*")
	if err != nil {
		return err
	}
	defer discardMetadataStage(temp)
	stream := &metadataStream{source: source}
	old, err := scanCommentPages(stream, temp, packet != nil)
	if err != nil {
		return err
	}
	if old.format != format {
		return errors.New("oggmeta: source codec changed after opening")
	}
	var delta uint32
	if packet != nil {
		delta, err = writeCommentPages(temp, old, packet)
		if err != nil {
			return err
		}
	}
	if err := copyMetadataAudio(temp, stream, delta); err != nil {
		return err
	}
	return copyMetadataStage(destination, temp)
}

func discardMetadataStage(file *os.File) {
	file.Close()
	os.Remove(file.Name())
}

func copyMetadataStage(destination io.Writer, file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(destination, file)
	return err
}
