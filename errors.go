package oggmeta

type ErrInvalidOggs struct{}

func (e *ErrInvalidOggs) Error() string {
	return "ogg header is missing OggS identifier"
}

type ErrBadSegs struct{}

func (e *ErrBadSegs) Error() string {
	return "ogg page has invalid number of segments"
}
