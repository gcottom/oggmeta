package oggmeta

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSavePreservesCustomCommentsAndOtherPages(t *testing.T) {
	for _, filename := range []string{"testdata-ogg-vorbis-nonEmpty.ogg", "testdata-opus-nonEmpty.ogg"} {
		t.Run(filename, func(t *testing.T) {
			src, err := os.ReadFile("testdata/" + filename)
			if err != nil {
				t.Fatal(err)
			}
			tag, err := ReadOGG(bytes.NewReader(src))
			if err != nil {
				t.Fatal(err)
			}
			vendor := tag.Vendor
			tag.SetTitle("A=B=C")
			tag.UnmappedFields = map[string]string{"CUSTOM": "x=y=z"}
			var dst bytes.Buffer
			if err = tag.Save(&dst); err != nil {
				t.Fatal(err)
			}
			updated, err := ReadOGG(bytes.NewReader(dst.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if updated.Title != "A=B=C" || updated.UnmappedFields["CUSTOM"] != "x=y=z" || updated.Vendor != vendor {
				t.Fatalf("comments lost: %+v", updated)
			}
			a, b := bytes.NewReader(src), bytes.NewReader(dst.Bytes())
			decodeA, decodeB := &OGGDecoder{Reader: a}, &OGGDecoder{Reader: b}
			changed := 0
			for {
				i, j := len(src)-a.Len(), dst.Len()-b.Len()
				pa, ea := decodeA.Decode()
				pb, eb := decodeB.Decode()
				if ea == io.EOF && eb == io.EOF {
					break
				}
				if ea != nil || eb != nil {
					t.Fatalf("decode: %v %v", ea, eb)
				}
				if pa.Header.SerialNumber != pb.Header.SerialNumber || pa.Header.PageSequenceNumber != pb.Header.PageSequenceNumber || pa.Header.Flags != pb.Header.Flags || pa.Header.GranulePosition != pb.Header.GranulePosition {
					t.Fatal("page header changed")
				}
				if !bytes.Equal(src[i:len(src)-a.Len()], dst.Bytes()[j:dst.Len()-b.Len()]) {
					changed++
				}
			}
			if changed != 1 {
				t.Fatalf("changed %d pages, want only the comment page", changed)
			}
			tag.SetTitle(strings.Repeat("x", MaxSegSequenceSize))
			dst.Reset()
			if err = tag.Save(&dst); err == nil || dst.Len() != 0 {
				t.Fatalf("oversized comment: %d bytes, err=%v", dst.Len(), err)
			}
		})
	}
}
func TestReadNoTagsAndBadChecksum(t *testing.T) {
	if _, err := ReadOGG(bytes.NewReader(nil)); err == nil {
		t.Fatal("empty input accepted")
	}
	src, err := os.ReadFile("testdata/testdata-opus.ogg")
	if err != nil {
		t.Fatal(err)
	}
	src[22] ^= 1
	if _, err := ReadOGG(bytes.NewReader(src)); err == nil {
		t.Fatal("bad checksum accepted")
	}
}

func TestSaveTruncatedMediaLeavesDestinationUntouched(t *testing.T) {
	src, err := os.ReadFile("testdata/testdata-opus-nonEmpty.ogg")
	if err != nil {
		t.Fatal(err)
	}
	tag, err := ReadOGG(bytes.NewReader(src[:len(src)-1]))
	if err != nil {
		t.Fatalf("opening complete comments: %v", err)
	}
	dst := bytes.NewBufferString("untouched")
	if err := tag.Save(dst); err != io.ErrUnexpectedEOF || dst.String() != "untouched" {
		t.Fatalf("save: %v, destination %q", err, dst.String())
	}
}
