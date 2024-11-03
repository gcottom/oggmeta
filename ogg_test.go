package oggmeta

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func compareImages(src1 [][][3]float32, src2 [][][3]float32) bool {
	dif := 0
	for i, dat1 := range src1 {
		for j := range dat1 {
			if len(src1[i][j]) != len(src2[i][j]) {
				dif++
			}
		}
	}
	return dif == 0
}

func image_2_array_at(src image.Image) [][][3]float32 {
	bounds := src.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	iaa := make([][][3]float32, height)

	for y := 0; y < height; y++ {
		row := make([][3]float32, width)
		for x := 0; x < width; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			// A color's RGBA method returns values in the range [0, 65535].
			// Shifting by 8 reduces this to the range [0, 255].
			row[x] = [3]float32{float32(r >> 8), float32(g >> 8), float32(b >> 8)}
		}
		iaa[y] = row
	}

	return iaa
}

func TestReadOggVorbisTags(t *testing.T) {
	path, _ := filepath.Abs("./testdata/test1.ogg")
	f, err := os.Open(path)
	assert.NoError(t, err)
	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	dec := &OGGDecoder{Reader: bytes.NewReader(b)}
	tag, err := dec.ReadTags()
	assert.NoError(t, err)
	assert.NotEmpty(t, tag.GetArtist())
	assert.NotEmpty(t, tag.GetAlbum())
	assert.NotEmpty(t, tag.GetTitle())
}
func TestReadOggOpusTags(t *testing.T) {
	path, _ := filepath.Abs("./testdata/testdata-opus.ogg")
	f, err := os.Open(path)
	assert.NoError(t, err)
	dec := &OGGDecoder{Reader: f}
	tag, err := dec.ReadTags()
	assert.NoError(t, err)
	assert.NotEmpty(t, tag.GetArtist())
	assert.NotEmpty(t, tag.GetAlbum())
	assert.NotEmpty(t, tag.GetTitle())
}

func TestWriteOggVorbis(t *testing.T) {
	t.Run("TestWriteOggVorbis-buffers", func(t *testing.T) {
		path, _ := filepath.Abs("./testdata/test1.ogg")
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()
		b, err := io.ReadAll(f)
		assert.NoError(t, err)
		r := bytes.NewReader(b)
		dec := &OGGDecoder{Reader: r}
		tag, err := dec.ReadTags()
		assert.NoError(t, err)
		tag.ClearAllTags()

		buffy := new(bytes.Buffer)
		err = SaveTags(tag, buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		dec.Reader = r
		tag, err = dec.ReadTags()
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		tag.SetBPM(127)
		tag.SetTrackNumber(3)
		tag.SetTrackTotal(12)

		buffy = new(bytes.Buffer)
		err = SaveTags(tag, buffy)
		assert.NoError(t, err)
		r = bytes.NewReader(buffy.Bytes())
		dec = &OGGDecoder{Reader: r}
		tag, err = dec.ReadTags()
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")
		assert.Equal(t, tag.GetBPM(), 127)
		assert.Equal(t, tag.GetTrackNumber(), 3)
		assert.Equal(t, tag.GetTrackTotal(), 12)
	})

	t.Run("TestWriteOggVorbis-file", func(t *testing.T) {
		err := os.Mkdir("./testdata/temp", 0755)
		assert.NoError(t, err)
		of, err := os.ReadFile("./testdata/test1.ogg")
		assert.NoError(t, err)
		err = os.WriteFile("./testdata/temp/test1.ogg", of, 0755)
		assert.NoError(t, err)
		path, _ := filepath.Abs("./testdata/temp/test1.ogg")
		f, err := os.Open(path)
		assert.NoError(t, err)
		defer f.Close()

		dec := &OGGDecoder{Reader: f}
		tag, err := dec.ReadTags()
		assert.NoError(t, err)
		tag.SetArtist("TestArtist1")
		tag.SetTitle("TestTitle1")
		tag.SetAlbum("TestAlbum1")
		tag.SetAlbumArtist("AlbumArtist1")
		tag.SetComposer("someone composed")
		tag.SetCopyright("please don't steal me")
		tag.SetEncoder("encoder da ba dee 23")
		tag.SetGenre("Metalcore")
		tag.SetTrackNumber(3)
		tag.SetTrackTotal(12)
		tag.SetDiscNumber(1)
		tag.SetDiscTotal(3)
		tag.SetBPM(127)
		p, err := filepath.Abs("./testdata/testdata-img-1.jpg")
		assert.NoError(t, err)
		jp, err := os.Open(p)
		assert.NoError(t, err)
		j, err := jpeg.Decode(jp)
		assert.NoError(t, err)
		tag.SetCoverArt(&j)
		assert.NoError(t, err)

		err = SaveTags(tag, f)
		assert.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		dec = &OGGDecoder{Reader: f}
		tag, err = dec.ReadTags()
		assert.NoError(t, err)
		err = os.RemoveAll("./testdata/temp")
		assert.NoError(t, err)
		assert.Equal(t, tag.GetArtist(), "TestArtist1")
		assert.Equal(t, tag.GetAlbum(), "TestAlbum1")
		assert.Equal(t, tag.GetTitle(), "TestTitle1")
		assert.Equal(t, tag.GetAlbumArtist(), "AlbumArtist1")
		assert.Equal(t, tag.GetComposer(), "someone composed")
		assert.Equal(t, tag.GetCopyright(), "please don't steal me")
		assert.Equal(t, tag.GetEncoder(), "encoder da ba dee 23")
		assert.Equal(t, tag.GetGenre(), "Metalcore")
		assert.Equal(t, tag.GetTrackNumber(), 3)
		assert.Equal(t, tag.GetTrackTotal(), 12)
		assert.Equal(t, tag.GetDiscNumber(), 1)
		assert.Equal(t, tag.GetDiscTotal(), 3)
		assert.Equal(t, tag.GetBPM(), 127)
		img1data := image_2_array_at(j)
		img2data := image_2_array_at(*tag.GetCoverArt())

		assert.True(t, compareImages(img1data, img2data))
	})
}
