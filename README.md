# OGGMeta

OGGMeta is a metadata reader and writer for OGG Vorbis and Opus files. It's built in pure go. A lot of research went into understanding the OGG format and vorbis comments. This library provides an interface for audiometa v3 to interact with OGG meta tags. 

## License
This project is licensed under the MIT License. See [LICENSE.md](LICENSE.md) for details.

## Related Links
[audiometa v3](https://github.com/gcottom/audiometa/v3)

[mp3meta](https://github.com/gcottom/mp3meta)

[mp4meta](https://github.com/gcottom/mp4meta)

[flacmeta](https://github.com/gcottom/flacmeta)

## Example

```go
in, err := os.Open("input.opus")
if err != nil { return err }
defer in.Close()

tag, err := oggmeta.ReadOGG(in)
if err != nil { return err }
tag.SetTitle("New title")

var output bytes.Buffer
if err := tag.Save(&output); err != nil { return err }
return os.WriteFile("output.opus", output.Bytes(), 0644)
```

Read from a seekable source at its beginning and keep it open through Save.
Save supports a single Vorbis or Opus logical stream with a complete comment
packet on one page. It rejects chained/multiplexed streams and replacements
that exceed the original page's lacing capacity. Non-comment pages remain
byte-identical. The legacy tag exposes one value per comment key and one cover
image. A legacy edit to a field replaces its repeated values. An edit to cover
art encodes the replacement as JPEG. Unedited comments and artwork retain
their original bytes, as do vendor identifiers and OpusTags trailing data.
Save stages the file on disk. Use a separate output when replacing the source
to avoid partially overwriting it on a destination write failure.

## Canonical metadata

`OpenMetadata` returns an AudioMeta editor for Vorbis and Opus files. It
supports typed values, repeated comment fields, and multiple artwork objects.

```go
import (
    "os"

    "github.com/gcottom/audiometa-core"
    "github.com/gcottom/oggmeta"
)

func revise(source, destination string) error {
    in, err := os.Open(source)
    if err != nil {
        return err
    }
    defer in.Close()

    metadata, err := oggmeta.OpenMetadata(in)
    if err != nil {
        return err
    }
    if err := metadata.SetField(core.Title, core.TextValue("New title")); err != nil {
        return err
    }
    if err := metadata.SetCustom("COLLECTION", "Archive", "Reference"); err != nil {
        return err
    }

    out, err := os.Create(destination)
    if err != nil {
        return err
    }
    saveErr := metadata.Save(out)
    closeErr := out.Close()
    if saveErr != nil {
        return saveErr
    }
    return closeErr
}
```

Choose a destination distinct from the source. The editor borrows its source
and destination and requires exclusive use while saving. `Save` stages output
in a temporary file before writing to the destination's current position.
Temporary disk space scales with the output. A destination write failure may
leave partial output. To replace the source, save to a distinct temporary file,
close both handles, then rename it to the source path.

The editor retains raw unrecognized comments and metadata it cannot decode.
Ordered values under a custom name use repeated `AUDIOMETA_1_CUSTOM_` comments.
Native artwork uses repeated `METADATA_BLOCK_PICTURE` comments and retains the
original image encoding. Deprecated `COVERART` and `COVERARTMIME` comments
are preserved as opaque metadata during ordinary edits. Unrelated edits preserve the vendor string,
comment framing and the optional binary data after the OpusTags comment list.
`ClearAll(true)` clears editable canonical and namespaced custom fields, with
opaque entries and OpusTags binary data preserved. `ClearAll(false)` also
clears opaque comment entries and optional padding. Signaled OpusTags binary
extensions, vendor identifiers,
Vorbis framing, and channel layout information are retained in both cases.

This editor supports one Vorbis or Opus logical stream. It rejects chained
or multiplexed streams when saving and accepts comment packets spanning
multiple pages, up to 64 MiB and 1,048,576 comment entries. Pages outside
the rewritten comment header retain their lacing, granules and packet data.
When the header gains or loses pages, subsequent page sequence numbers
and checksums are updated. A save with no pending changes copies valid
single-stream input byte for byte.

Run the independent-tool checks with `go test -tags integration ./...` and
FFmpeg and FFprobe on PATH. To also exercise native metadata and picture
interoperability, set `AUDIOMETA_MUTAGEN_PYTHON` to a Python executable with
Mutagen available and `AUDIOMETA_EXIFTOOL` to the ExifTool executable.

See [Ogg encapsulation in the Vorbis I specification](https://xiph.org/vorbis/doc/Vorbis_I_spec.html),
[RFC 7845 for Ogg Opus](https://www.rfc-editor.org/rfc/rfc7845.html#section-5.2),
and [the Xiph cover art convention](https://wiki.xiph.org/VorbisComment#Cover_art).
