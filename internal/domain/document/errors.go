package document

import "errors"

// Sentinel errors (m1-plan §4.5), mapped to problem types in internal/http/respond/errmap.go.
var (
	ErrInvalidFile      = errors.New("document: invalid file")
	ErrFileTooLarge     = errors.New("document: file too large")
	ErrUnsupportedMedia = errors.New("document: unsupported media type")
	ErrLinkExpired      = errors.New("document: download link expired or invalid")
	ErrVersionMismatch  = errors.New("document: version mismatch")
)
