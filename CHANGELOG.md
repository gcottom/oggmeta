# Changelog

## v2.0.0

- Use `github.com/gcottom/oggmeta/v2` as the Go module path and depend on AudioMeta Core v2.0.0.

- Added the typed `OpenMetadata` API for Vorbis and Opus.
- Added lossless multi-value comments and multi-picture editing through the
  canonical API, including comment packets that span multiple Ogg pages.
- Preserved the vendor string, opaque comments, OpusTags ancillary data and
  codec packets across ordinary canonical edits.
- Preserved untouched comments, artwork, and OpusTags ancillary data during
  legacy saves, with temporary-file staging.
- Added FFmpeg and FFprobe audio integrity checks and independent tag
  verification with Mutagen and ExifTool.
