package auth

import (
	"context"
	"errors"
	"testing"
)

// TestAuthorizeAny_NoPermissions: an empty permission list is refused before any lookup.
func TestAuthorizeAny_NoPermissions(t *testing.T) {
	p := &Principal{}
	if _, _, err := p.AuthorizeAny(context.Background(), nil, "m"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	var nilP *Principal
	if _, _, err := nilP.AuthorizeAny(context.Background(), nil, "m", PermYardRead); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("nil principal: %v", err)
	}
}
