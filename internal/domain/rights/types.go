package rights

// Right kinds (berth_rights.kind).
const (
	KindOwnershipShare = "ownership_share"
	KindLongTermUse    = "long_term_use"
	KindMembership     = "membership"
)

// Holder instalment sources (holder_installments.source_type).
const (
	SourceManagementFee = "management_fee"
	SourceCommonAdvance = "common_advance"
	SourceCommonBalance = "common_balance"
	SourceTransferFee   = "transfer_fee"
)

// Requirement policies of transfers.
const (
	RequirementBlock = "block"
	RequirementWarn  = "warn"
)

// Fee policies of transfers.
const (
	FeesReassign = "reassign"
	FeesProRata  = "prorata"
)
