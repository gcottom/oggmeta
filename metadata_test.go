package oggmeta

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gcottom/audiometa-core"
	"github.com/gcottom/audiometa-core/contracttest"
)

func TestCanonicalOggAdapterContract(t *testing.T) {
	for codec, filename := range map[string]string{
		Vorbis: "testdata-ogg-vorbis-nonEmpty.ogg",
		Opus:   "testdata-opus-nonEmpty.ogg",
	} {
		t.Run(codec, func(t *testing.T) {
			contracttest.Run(t, OpenMetadata, loadMetadataFixture(t, filename))
		})
	}
}

func TestMetadataOggGrowthAndPreservation(t *testing.T) {
	for _, test := range []struct {
		codec, fixture string
		trailer        []byte
	}{
		{Vorbis, "testdata-ogg-vorbis-nonEmpty.ogg", []byte{1}},
		{Opus, "testdata-opus-nonEmpty.ogg", []byte{1, 9, 8, 7}},
		{Opus, "testdata-opus-nonEmpty.ogg", []byte{0, 0, 0}},
	} {
		t.Run(test.codec, func(t *testing.T) {
			source := loadMetadataFixture(t, test.fixture)
			entries := []string{"TITLE=Old", "FuTuRe=first", "future=second", "no separator", pictureComment + "=malformed!"}
			packet, err := encodeCommentMetadata(commentPrefix(test.codec), "", entries, test.trailer)
			if err != nil {
				t.Fatal(err)
			}
			var seeded bytes.Buffer
			if err := saveMetadataStream(bytes.NewReader(source), &seeded, test.codec, packet); err != nil {
				t.Fatal(err)
			}

			editor, err := OpenMetadata(bytes.NewReader(seeded.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			var pristine bytes.Buffer
			if err := editor.Save(&pristine); err != nil || !bytes.Equal(pristine.Bytes(), seeded.Bytes()) {
				t.Fatalf("no-op changed bytes, err=%v", err)
			}
			const title = "雪=音楽"
			if err := editor.SetField(core.Title, core.TextValue(title)); err != nil {
				t.Fatal(err)
			}
			if err := editor.SetCustom("collection", "", "Αρχείο"); err != nil {
				t.Fatal(err)
			}
			art := metadataFixtureArt(t)
			small := core.PictureValue(core.Picture{
				Role: 4, MIME: "-->", Description: "back", Data: []byte("https://example.com/back.png"),
			})
			if err := editor.SetField(core.Artwork, art, small); err != nil {
				t.Fatal(err)
			}
			var rendered, repeat bytes.Buffer
			if err := editor.Save(&rendered); err != nil {
				t.Fatal(err)
			}
			if err := editor.Save(&repeat); err != nil || !bytes.Equal(rendered.Bytes(), repeat.Bytes()) {
				t.Fatalf("repeated save changed output, err=%v", err)
			}
			readback, err := OpenMetadata(bytes.NewReader(rendered.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := readback.GetField(core.Title); !ok || !got.Equal(core.TextValue(title)) {
				t.Fatalf("title: %v, %v", got, ok)
			}
			if got, ok := readback.GetCustom("COLLECTION"); !ok || !reflect.DeepEqual(got, []string{"", "Αρχείο"}) {
				t.Fatalf("custom values: %q", got)
			}
			gotArtwork, _ := readback.Get(core.Artwork)
			contracttest.AssertValues(t, gotArtwork, []core.Value{art, small})
			after, position := extractMetadataComments(t, rendered.Bytes())
			if position < 2 {
				t.Fatal("picture did not grow the comment packet across a page boundary")
			}
			if after.vendor != "" || !bytes.Equal(after.trailer, test.trailer) {
				t.Fatalf("vendor/trailer changed: %q / %v", after.vendor, after.trailer)
			}
			if got := retainedEntries(after.entries, entries[1:]); !reflect.DeepEqual(got, entries[1:]) {
				t.Fatalf("opaque comments changed: %q", got)
			}
			requireUnchangedOggPayload(t, seeded.Bytes(), rendered.Bytes())

			readback.ClearAll(true)
			var preserved bytes.Buffer
			if err := readback.Save(&preserved); err != nil {
				t.Fatal(err)
			}
			after, _ = extractMetadataComments(t, preserved.Bytes())
			if !reflect.DeepEqual(after.entries, entries[1:]) || !bytes.Equal(after.trailer, test.trailer) {
				t.Fatalf("preserve-unknown clear: entries=%d retained=%q trailer=%v",
					len(after.entries), retainedEntries(after.entries, entries[1:]), after.trailer)
			}
			requireUnchangedOggPayload(t, seeded.Bytes(), preserved.Bytes())

			readback.ClearAll(false)
			var cleared bytes.Buffer
			if err := readback.Save(&cleared); err != nil {
				t.Fatal(err)
			}
			after, _ = extractMetadataComments(t, cleared.Bytes())
			expectedTrailer := test.trailer
			if test.codec == Opus && expectedTrailer[0]&1 == 0 {
				expectedTrailer = nil
			}
			if len(after.entries) != 0 || !bytes.Equal(after.trailer, expectedTrailer) {
				t.Fatalf("full clear left %d comments or changed protected trailer: %x", len(after.entries), after.trailer)
			}
			requireUnchangedOggPayload(t, seeded.Bytes(), cleared.Bytes())
		})
	}
}

func TestMetadataOggWriteFailures(t *testing.T) {
	source := loadMetadataFixture(t, "testdata-opus-nonEmpty.ogg")
	for name, input := range map[string][]byte{
		"truncated": source[:len(source)-1],
		"chained":   append(append([]byte(nil), source...), source...),
	} {
		t.Run(name, func(t *testing.T) {
			editor, err := OpenMetadata(bytes.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			dst := bytes.NewBufferString("original")
			if err := editor.Save(dst); err == nil || dst.String() != "original" {
				t.Fatalf("save error=%v; output len=%d", err, dst.Len())
			}
		})
	}
	editor, err := OpenMetadata(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.Save(shortMetadataWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected short write, got %v", err)
	}

	filename := filepath.Join(t.TempDir(), "source.opus")
	if err := os.WriteFile(filename, source, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	editor, err = OpenMetadata(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.Save(r); err == nil {
		t.Fatal("accepted same source and destination")
	}
	now, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(source, now) {
		t.Fatalf("source modified, err=%v", err)
	}
}

func TestLegacySavePreservesUnchangedMetadata(t *testing.T) {
	source := loadMetadataFixture(t, "testdata-opus-nonEmpty.ogg")
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	picture := core.Picture{Role: 4, MIME: "image/png", Description: "back", Width: 8, Height: 8, Data: pngData.Bytes()}
	block, err := core.MarshalPictureBlock(picture)
	if err != nil {
		t.Fatal(err)
	}
	comment := pictureComment + "=" + base64.StdEncoding.EncodeToString(block)
	trailer := []byte{1, 7, 3, 2}
	original := []string{"TITLE=Original", "unknown=first", "UNknown=last", "opaque", comment}
	packet, err := encodeCommentMetadata(commentPrefix(Opus), "", original, trailer)
	if err != nil {
		t.Fatal(err)
	}
	var fixture bytes.Buffer
	if err := saveMetadataStream(bytes.NewReader(source), &fixture, Opus, packet); err != nil {
		t.Fatal(err)
	}
	tag, err := ReadOGG(bytes.NewReader(fixture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	// Change the same exported field directly as historical callers can do.
	tag.Title = "Edited"
	var output bytes.Buffer
	if err := tag.Save(&output); err != nil {
		t.Fatal(err)
	}
	got, _ := extractMetadataComments(t, output.Bytes())
	want := append([]string{"TITLE=Edited"}, original[1:]...)
	if !reflect.DeepEqual(got.entries, want) || got.vendor != "" || !bytes.Equal(got.trailer, trailer) {
		t.Fatal("legacy save changed untouched comment bytes, vendor or Opus trailer")
	}
	requireUnchangedOggPayload(t, fixture.Bytes(), output.Bytes())

	tag.UnmappedFields["UNKNOWN"] = "replacement"
	img, ok := (*tag.CoverArt).(*image.NRGBA)
	if !ok {
		t.Fatalf("unexpected PNG decoder type %T", *tag.CoverArt)
	}
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	output.Reset()
	if err := tag.Save(&output); err != nil {
		t.Fatal(err)
	}
	got, _ = extractMetadataComments(t, output.Bytes())
	if len(got.entries) != 4 || got.entries[1] != "UNKNOWN=replacement" {
		t.Fatalf("direct map edit left %d entries", len(got.entries))
	}
	key, value, _ := strings.Cut(got.entries[3], "=")
	result, err := parsePictureComment(value)
	if err != nil || key != pictureComment || result.MIME != "image/jpeg" || bytes.Equal(result.Data, picture.Data) {
		t.Fatalf("direct image edit was not rendered, err=%v", err)
	}
}

type shortMetadataWriter struct{}

func (shortMetadataWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func loadMetadataFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func metadataFixtureArt(t testing.TB) core.Value {
	t.Helper()
	// An uncompressed PNG guarantees a real picture larger than one Ogg page.
	const width, height = 164, 164
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.SetRGBA(x, y, color.RGBA{R: byte(x), G: byte(y), B: byte(x * y), A: 255})
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return core.PictureValue(core.Picture{
		Role: 3, MIME: "image/png", Description: "表紙", Width: width, Height: height, Depth: 24, Data: encoded.Bytes(),
	})
}

func retainedEntries(entries, expected []string) []string {
	var found []string
	for _, entry := range entries {
		for _, value := range expected {
			if entry == value {
				found = append(found, entry)
				break
			}
		}
	}
	return found
}

func extractMetadataComments(t testing.TB, data []byte) (commentMetadata, uint32) {
	t.Helper()
	s := &metadataStream{source: bytes.NewReader(data)}
	headers, err := scanCommentPages(s, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	prefix := commentPrefix(headers.format)
	metadata, err := parseCommentMetadata(headers.packet[len(prefix):], headers.format)
	if err != nil {
		t.Fatal(err)
	}
	return metadata, s.next - headers.firstNumber
}

func requireUnchangedOggPayload(t *testing.T, first, second []byte) {
	t.Helper()
	a := &metadataStream{source: bytes.NewReader(first)}
	b := &metadataStream{source: bytes.NewReader(second)}
	headersA, errA := scanCommentPages(a, nil, false)
	headersB, errB := scanCommentPages(b, nil, false)
	if errA != nil || errB != nil {
		t.Fatalf("headers: %v, %v", errA, errB)
	}
	if !bytes.Equal(headersA.tailLaces, headersB.tailLaces) || !bytes.Equal(headersA.tailPayload, headersB.tailPayload) {
		t.Fatal("packets after comment header changed")
	}
	for {
		pageA, errA := a.readPage()
		pageB, errB := b.readPage()
		if errA == io.EOF && errB == io.EOF {
			return
		}
		if errA != nil || errB != nil {
			t.Fatalf("media pages: %v, %v", errA, errB)
		}
		if !bytes.Equal(pageA[:18], pageB[:18]) || !bytes.Equal(pageA[26:], pageB[26:]) {
			t.Fatal("audio page changed beyond sequence number and checksum")
		}
	}
}

func FuzzCommentMetadata(f *testing.F) {
	for _, format := range []string{Vorbis, Opus} {
		trailer := []byte{1}
		prefix := commentPrefix(format)
		packet, err := encodeCommentMetadata(prefix, "vendor", []string{"TITLE=Text", "KEY=a", "KEY=b"}, trailer)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(packet[len(prefix):], format == Opus)
	}
	f.Fuzz(func(t *testing.T, packet []byte, opus bool) {
		if len(packet) > 1<<20 {
			t.Skip()
		}
		format := Vorbis
		if opus {
			format = Opus
		}
		data, err := parseCommentMetadata(packet, format)
		if err != nil {
			return
		}
		prefix := commentPrefix(format)
		result, err := encodeCommentMetadata(prefix, data.vendor, data.entries, data.trailer)
		if err != nil || !bytes.Equal(packet, result[len(prefix):]) {
			t.Fatalf("valid comment packet changed on re-encode, err=%v", err)
		}
	})
}

func BenchmarkMetadataSave(b *testing.B) {
	source := loadMetadataFixture(b, "testdata-opus-nonEmpty.ogg")
	editor, err := OpenMetadata(bytes.NewReader(source))
	if err != nil {
		b.Fatal(err)
	}
	if err := editor.SetField(core.Title, core.TextValue(strings.Repeat("title", 16))); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := editor.Save(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
