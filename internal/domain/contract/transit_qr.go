package contract

import (
	"context"
	"errors"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// TransitQrPrefix starts the payload of a transit QR (m6b-plan §6.4, D10): PT1:<contract_id>. The payload is
// opaque (no personal data, no URL) and only an authenticated panel of the same marina can resolve it.
const TransitQrPrefix = "PT1:"

// TransitQrPayload returns the QR payload of a transit (contracts.read or transits.desk). Contracts that are not
// transits are ErrNotFound, like another marina's contracts.
func (s *Service) TransitQrPayload(ctx context.Context, p *auth.Principal, marinaID, id string) (string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	}
	if err != nil {
		return "", err
	}
	c, err := store.GetContract(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return "", err
	}
	if c.Kind != KindTransit {
		return "", ErrNotFound
	}
	return TransitQrPrefix + c.ID, nil
}
