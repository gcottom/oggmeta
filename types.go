package ogg

import "io"

const (
	Vorbis = "vorbis"
	Opus   = "opus"
)

const (
	HeaderSize         = 27
	MaxSegSize         = 255
	MaxSegSequenceSize = MaxSegSize * 255
	MaxPageSize        = HeaderSize + MaxSegSequenceSize + MaxSegSize
)

const (
	FlagCOP = 1 << iota // Continuation of packet
	FlagBOS             // Beginning of stream
	FlagEOS             // End of stream
)

var (
	Oggs         = [4]byte{'O', 'g', 'g', 'S'}
	VorbisPrefix = []byte("\x03vorbis")
	OpusPrefix   = []byte("OpusTags")
)

var dummyPacket = [1][]byte{{}}

var tagFieldMapping = map[string]string{"ALBUM": "Album", "ALBUMARTIST": "AlbumArtist", "ARTIST": "Artist", "BPM": "BPM", "COMPOSER": "Composer", "COPYRIGHT": "Copyright", "DISCNUMBER": "DiscNumber", "DISCTOTAL": "DiscTotal", "ENCODER": "Encoder", "GENRE": "Genre", "TITLE": "Title", "TRACKNUMBER": "TrackNumber", "TRACKTOTAL": "TrackTotal"}

type OGGPageHeader struct {
	Oggs               [4]byte
	Version            byte
	Flags              byte
	GranulePosition    int64
	SerialNumber       uint32
	PageSequenceNumber uint32
	CRC                uint32
	Segments           byte
}

// A page can contain multiple packets

type OGGPage struct {
	Header  OGGPageHeader
	Packets [][]byte
}

type OGGDecoder struct {
	Reader    io.ReadSeeker
	TagReader io.ReadSeeker
}

type OGGEncoder struct {
	Writer     io.Writer
	Serial     uint32
	PageNumber uint32
}

type segmentizePayload struct {
	leftPay   []byte
	middlePay [][]byte
	rightPay  []byte
}
