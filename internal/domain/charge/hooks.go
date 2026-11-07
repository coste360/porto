package charge

import (
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/rights"
)

// ContractHooks returns the hooks registered on the contract service (m5-plan §8.1): *Service implements
// contract.BillingHooks (origins.go), so issued instalments, confirmed transits, reprices and closes create or
// correct charges in the contract's transaction.
func (s *Service) ContractHooks() contract.BillingHooks { return s }

// RightsHooks returns the hooks registered on the rights service (holder instalments).
func (s *Service) RightsHooks() rights.BillingHooks { return s }
