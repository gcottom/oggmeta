package ogg

import (
	"fmt"
	"image"
	"io"
	"strconv"
)

type OggTag struct {
	Album          string
	AlbumArtist    string
	Artist         string
	BPM            string
	Codec          string
	Composer       string
	Copyright      string
	CoverArt       *image.Image
	DiscNumber     string
	DiscTotal      string
	Encoder        string
	Genre          string
	Title          string
	TrackNumber    string
	TrackTotal     string
	UnmappedFields map[string]string
	Vendor         string

	reader io.ReadSeeker
}

func (o *OggTag) ClearAllTags() {
	o.Album = ""
	o.AlbumArtist = ""
	o.Artist = ""
	o.BPM = ""
	o.Composer = ""
	o.Copyright = ""
	o.CoverArt = nil
	o.DiscNumber = ""
	o.DiscTotal = ""
	o.Encoder = ""
	o.Genre = ""
	o.Title = ""
	o.TrackNumber = ""
	o.TrackTotal = ""
}

func (o *OggTag) GetAlbum() string {
	return o.Album
}

func (o *OggTag) GetAlbumArtist() string {
	return o.AlbumArtist
}

func (o *OggTag) GetArtist() string {
	return o.Artist
}

func (o *OggTag) GetBPM() int {
	bpm, err := strconv.Atoi(o.BPM)
	if err != nil {
		return 0
	}
	return bpm
}

func (o *OggTag) GetComposer() string {
	return o.Composer
}

func (o *OggTag) GetCopyright() string {
	return o.Copyright
}

func (o *OggTag) GetCoverArt() *image.Image {
	return o.CoverArt
}

func (o *OggTag) GetDiscNumber() int {
	discNumber, err := strconv.Atoi(o.DiscNumber)
	if err != nil {
		return 0
	}
	return discNumber
}

func (o *OggTag) GetDiscTotal() int {
	discTotal, err := strconv.Atoi(o.DiscTotal)
	if err != nil {
		return 0
	}
	return discTotal
}

func (o *OggTag) GetEncoder() string {
	return o.Encoder
}

func (o *OggTag) GetGenre() string {
	return o.Genre
}

func (o *OggTag) GetTitle() string {
	return o.Title
}

func (o *OggTag) GetTrackNumber() int {
	trackNumber, err := strconv.Atoi(o.TrackNumber)
	if err != nil {
		return 0
	}
	return trackNumber
}

func (o *OggTag) GetTrackTotal() int {
	trackTotal, err := strconv.Atoi(o.TrackTotal)
	if err != nil {
		return 0
	}
	return trackTotal
}

func (o *OggTag) SetAlbum(album string) {
	o.Album = album
}

func (o *OggTag) SetAlbumArtist(albumArtist string) {
	o.AlbumArtist = albumArtist
}

func (o *OggTag) SetArtist(artist string) {
	o.Artist = artist
}

func (o *OggTag) SetBPM(bpm int) {
	o.BPM = fmt.Sprintf("%d", bpm)
}

func (o *OggTag) SetComposer(composer string) {
	o.Composer = composer
}

func (o *OggTag) SetCopyright(copyright string) {
	o.Copyright = copyright
}

func (o *OggTag) SetCoverArt(coverArt *image.Image) {
	o.CoverArt = coverArt
}

func (o *OggTag) SetDiscNumber(discNumber int) {
	o.DiscNumber = fmt.Sprintf("%d", discNumber)
}

func (o *OggTag) SetDiscTotal(discTotal int) {
	o.DiscTotal = fmt.Sprintf("%d", discTotal)
}

func (o *OggTag) SetEncoder(encoder string) {
	o.Encoder = encoder
}

func (o *OggTag) SetGenre(genre string) {
	o.Genre = genre
}

func (o *OggTag) SetTitle(title string) {
	o.Title = title
}

func (o *OggTag) SetTrackNumber(trackNumber int) {
	o.TrackNumber = fmt.Sprintf("%d", trackNumber)
}

func (o *OggTag) SetTrackTotal(trackTotal int) {
	o.TrackTotal = fmt.Sprintf("%d", trackTotal)
}
