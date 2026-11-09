package profile

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until PUBLIC lands the bodies (501).
	ErrNotImplemented = errors.New("profile: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("profile: version mismatch")
	// ErrPhotoInvalid is an upload that does not decode as JPEG or PNG (422 photo_invalid).
	ErrPhotoInvalid = errors.New("profile: photo invalid")
	// ErrNotPublished is an unpublished profile, an unknown slug, a section switched off or a photo not referenced
	// by the published profile: every public operation answers 404 not_found with the same body.
	ErrNotPublished = errors.New("profile: not published")
)

// IncompleteError is a publish refused for missing data (422 profile_incomplete, missing in the details):
// position, contact, name.
type IncompleteError struct{ Missing []string }

func (e *IncompleteError) Error() string { return "profile: incomplete" }
