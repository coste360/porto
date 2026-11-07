package docs

import (
	"errors"
	"fmt"
)

// Sentinel errors (m4-plan §7.4; names fixed), mapped in internal/http/respond/errmap.go.
var (
	// ErrTemplateInvalid means a template does not validate against the catalogue of its kind; the error is a
	// *TemplateError with the positions. 422 template_invalid.
	ErrTemplateInvalid = errors.New("docs: template invalid")
	// ErrTemplateLimits means the output or the loop iterations exceed the limits. 422 template_limits.
	ErrTemplateLimits = errors.New("docs: template limits exceeded")
	// ErrNotReady means the document is not rendered yet (pending, rendering or failed). 409 not_ready.
	ErrNotReady = errors.New("docs: document not ready")
	// ErrCodeTaken means another template of the marina has the code. 409 code_taken.
	ErrCodeTaken = errors.New("docs: template code taken")
	// ErrKindNotPublishable means the kind is in the catalogue for a later milestone. 422 validation_failed.
	ErrKindNotPublishable = errors.New("docs: kind not publishable yet")
	// ErrNoDataSource means no data source is registered for a kind (wiring bug). 500.
	ErrNoDataSource = errors.New("docs: no data source for kind")
	// ErrImmutable means a published version or a signed document cannot change. 409 conflict.
	ErrImmutable = errors.New("docs: immutable")
)

// TemplateIssue is one error of a template (line and column are 1-based).
type TemplateIssue struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Code string `json:"code"` // unknown_path, filter_type, each_not_list, syntax, unclosed_block, ...
	Path string `json:"path,omitempty"`
}

// TemplateError carries the issues of an invalid template (details of 422 template_invalid).
type TemplateError struct {
	Issues []TemplateIssue
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("%s: %d issue(s)", ErrTemplateInvalid.Error(), len(e.Issues))
}

// Is makes errors.Is(err, ErrTemplateInvalid) true.
func (e *TemplateError) Is(target error) bool { return target == ErrTemplateInvalid }
