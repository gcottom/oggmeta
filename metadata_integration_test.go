//go:build integration
// +build integration

package oggmeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gcottom/audiometa-core"
)

func TestMetadataOggIndependentTools(t *testing.T) {
	for _, test := range []struct {
		ext, encoder string
	}{
		{"ogg", "libvorbis"},
		{"opus", "libopus"},
	} {
		t.Run(test.ext, func(t *testing.T) {
			dir := t.TempDir()
			first := filepath.Join(dir, "source."+test.ext)
			second := filepath.Join(dir, "edited."+test.ext)
			runMetadataTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
				"sine=frequency=765:sample_rate=48000:duration=0.4", "-ac", "2",
				"-c:a", test.encoder, "-metadata", "title=Original", first)

			input, err := os.Open(first)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			editor, err := OpenMetadata(input)
			if err != nil {
				t.Fatal(err)
			}
			art := metadataFixtureArt(t)
			timed := core.TimedEventValue(core.TimedEvent{StartMS: 125, ContentType: "application/octet-stream", Data: []byte{0, 1, 2}})
			for field, value := range map[core.Field]core.Value{
				core.Title:         core.TextValue("Page growth"),
				core.Mood:          core.TextValue("Bright"),
				core.TrackNumber:   core.IntValue(3),
				core.Artwork:       art,
				core.TimedMetadata: timed,
			} {
				if err := editor.SetField(field, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := editor.SetCustom("COLLECTION", "One", "Two"); err != nil {
				t.Fatal(err)
			}
			output, err := os.Create(second)
			if err != nil {
				t.Fatal(err)
			}
			saveErr := editor.Save(output)
			closeErr := output.Close()
			if saveErr != nil || closeErr != nil {
				t.Fatalf("save=%v close=%v", saveErr, closeErr)
			}

			for _, check := range []struct {
				label, tool string
				args        []string
			}{
				{"decoded PCM SHA-256", "ffmpeg", []string{
					"-xerror", "-v", "error", "-map", "0:a:0", "-c:a", "pcm_s16le", "-f", "hash", "-hash", "sha256", "-",
				}},
				{"codec, sample rate, channels, duration", "ffprobe", []string{
					"-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate,channels,duration", "-of", "json",
				}},
			} {
				withInput := func(path string) []string {
					if check.tool == "ffmpeg" {
						return append([]string{"-i", path}, check.args...)
					}
					return append(append([]string(nil), check.args...), path)
				}
				before := runMetadataTool(t, check.tool, withInput(first)...)
				after := runMetadataTool(t, check.tool, withInput(second)...)
				if len(before) == 0 || !bytes.Equal(before, after) {
					t.Fatalf("%s changed:\n%s\n%s", check.label, before, after)
				}
				t.Logf("%s matched (%d bytes of tool output)", check.label, len(after))
			}
			before := metadataPacketHashes(t, first)
			after := metadataPacketHashes(t, second)
			if len(before) == 0 || !reflect.DeepEqual(before, after) {
				t.Fatal("compressed audio packet hashes changed")
			}
			t.Logf("SHA-256 matched for %d compressed audio packets", len(after))

			if python := os.Getenv("AUDIOMETA_MUTAGEN_PYTHON"); python != "" {
				picture, _ := art.Picture()
				pictureHash := fmt.Sprintf("%x", sha256.Sum256(picture.Data))
				wire, err := core.Encode(core.TimedMetadata, timed)
				if err != nil {
					t.Fatal(err)
				}
				runMetadataTool(t, python, "-c", `import base64, sys
from hashlib import sha256
from mutagen import File
from mutagen.flac import Picture
meta = File(sys.argv[1]).tags
assert meta["title"] == ["Page growth"], meta["title"]
assert meta["mood"] == ["Bright"], meta["mood"]
assert meta["tracknumber"] == ["3"], meta["tracknumber"]
assert meta["AUDIOMETA_1_CUSTOM_COLLECTION"] == ["One", "Two"]
assert meta[sys.argv[3]] == [sys.argv[4]]
picture, = meta["metadata_block_picture"]
image = Picture(base64.b64decode(picture))
assert image.type == 3 and image.mime == "image/png"
assert image.width == 164 and image.height == 164 and image.desc == "表紙"
assert sha256(image.data).hexdigest() == sys.argv[2]
`, second, pictureHash, core.CommentFallbackName(core.TimedMetadata), wire)
				t.Log("Mutagen verified native comments, custom repetitions, fallback payload and encoded artwork SHA-256")
			}
			if exiftool := os.Getenv("AUDIOMETA_EXIFTOOL"); exiftool != "" {
				result := runMetadataTool(t, exiftool, "-s3", "-Title", second)
				if !bytes.Contains(result, []byte("Page growth")) {
					t.Fatalf("ExifTool title: %s", result)
				}
				t.Log("ExifTool read the updated native title")
			}
		})
	}
}

func metadataPacketHashes(t *testing.T, filename string) []string {
	t.Helper()
	raw := runMetadataTool(t, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_packets", "-show_data_hash", "sha256", "-show_entries", "packet=data_hash", "-of", "json", filename)
	var packets struct {
		Packets []struct {
			Hash string `json:"data_hash"`
		}
	}
	if err := json.Unmarshal(raw, &packets); err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, 0, len(packets.Packets))
	for _, packet := range packets.Packets {
		if len(packet.Hash) != len("SHA256:")+64 {
			t.Fatalf("invalid packet SHA-256 %q", packet.Hash)
		}
		hashes = append(hashes, packet.Hash)
	}
	return hashes
}

func runMetadataTool(t testing.TB, tool string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(tool, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s\n%s", tool, args, err, stderr.Bytes(), data)
	}
	return data
}

func BenchmarkMetadataMusicLength(b *testing.B) {
	dir := b.TempDir()
	filename := filepath.Join(dir, "music-length.opus")
	runMetadataTool(b, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=493:sample_rate=48000:duration=180", "-ac", "2",
		"-c:a", "libopus", "-b:a", "160k", filename)
	input, err := os.Open(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer input.Close()
	stat, err := input.Stat()
	if err != nil {
		b.Fatal(err)
	}
	editor, err := OpenMetadata(input)
	if err != nil {
		b.Fatal(err)
	}
	if err := editor.SetField(core.Title, core.TextValue("Revised")); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(stat.Size())
	b.Logf("source size %d bytes", stat.Size())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := editor.Save(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
