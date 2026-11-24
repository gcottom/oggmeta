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
byte-identical. The public tag represents only one value per comment key;
repeated keys collapse, and embedded cover art is re-encoded as JPEG.
Save buffers the rewritten file and is not atomic; prefer a separate output.
