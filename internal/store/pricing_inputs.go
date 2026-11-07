package store

import (
	"context"
	"slices"

	"github.com/uptrace/bun"
)

// Read-only lookups of M1 rows that the pricing tools consume (m2-plan §4.5, §5.1). They live here so the M1
// store files stay untouched. Nothing here returns names or contact data.

// PricingResource is what pricing needs of a berth, dynamic area or land resource.
type PricingResource struct {
	ID         string
	Type       string // berth, dynamic_area, land_resource
	PriceClass string
	LengthCm   int64 // berth: length; area: max LOA; land: length
	WidthCm    int64 // berth: width; area: max beam; land: width
	MaxLOACm   int64
	MaxBeamCm  int64
	GroupIDs   []string
}

// PricingResourceByID returns a resource of the marina by type and id, with its group ids.
func PricingResourceByID(ctx context.Context, db bun.IDB, s MarinaScope, typ, id string) (*PricingResource, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	out := &PricingResource{ID: id, Type: typ}
	switch typ {
	case "berth":
		b := new(Berth)
		if err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
			return nil, notFound(err)
		}
		out.PriceClass, out.LengthCm, out.WidthCm, out.MaxLOACm, out.MaxBeamCm = b.PriceClass, b.LengthCm, b.WidthCm, b.MaxLOACm, b.MaxBeamCm
	case "dynamic_area":
		a := new(DynamicArea)
		if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
			return nil, notFound(err)
		}
		out.PriceClass, out.LengthCm, out.WidthCm, out.MaxLOACm, out.MaxBeamCm = a.PriceClass, a.MaxLOACm, a.MaxBeamCm, a.MaxLOACm, a.MaxBeamCm
	case "land_resource":
		l := new(LandResource)
		if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
			return nil, notFound(err)
		}
		out.PriceClass, out.LengthCm, out.WidthCm = l.PriceClass, l.LengthCm, l.WidthCm
	default:
		return nil, ErrNotFound
	}
	ids := []string{}
	err := db.NewSelect().Table("berth_group_members").Column("group_id").Where("marina_id = ?", s.ID()).
		Where("resource_type = ?", typ).Where("resource_id = ?", id).OrderExpr("group_id").Scan(ctx, &ids)
	if err != nil {
		return nil, err
	}
	out.GroupIDs = ids
	return out, nil
}

// PricingVessel is what pricing needs of a vessel.
type PricingVessel struct {
	ID                            string
	HullType                      string
	LOACm, BeamCm                 int64
	MeasuredLOACm, MeasuredBeamCm int64
	DimensionsSource              string // declared, measured
}

// PricingVesselByID returns a vessel of the marina.
func PricingVesselByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PricingVessel, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	v := new(Vessel)
	if err := db.NewSelect().Model(v).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return &PricingVessel{
		ID: v.ID, HullType: v.HullType, LOACm: v.LOACm, BeamCm: v.BeamCm, MeasuredLOACm: v.MeasuredLOACm,
		MeasuredBeamCm: v.MeasuredBeamCm, DimensionsSource: v.DimensionsSource,
	}, nil
}

// CustomerCategoryByID returns the category of a customer of the marina ("" when none).
func CustomerCategoryByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (string, error) {
	if !ValidID(id) {
		return "", ErrNotFound
	}
	var cat *string
	err := db.NewSelect().Table("customers").Column("category").Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Limit(1).Scan(ctx, &cat)
	if err != nil {
		return "", notFound(err)
	}
	if cat == nil {
		return "", nil
	}
	return *cat, nil
}

// CustomerCategoryCounts returns the number of non-archived customers per category ("" = none).
func CustomerCategoryCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	var rows []struct {
		Category *string `bun:"category"`
		N        int     `bun:"n"`
	}
	err := db.NewSelect().Table("customers").Column("category").ColumnExpr("COUNT(*) AS n").Where("marina_id = ?", s.ID()).
		Where("archived_at IS NULL").Group("category").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		k := ""
		if r.Category != nil {
			k = *r.Category
		}
		out[k] += r.N
	}
	return out, nil
}

// PriceClassDims are the largest dimensions of the active elements of one price_class code.
type PriceClassDims struct {
	Code                    string `bun:"code"`
	MaxLOACm, MaxBeamCm     int64
	MaxLengthCm, MaxWidthCm int64
}

// PriceClassMaxDims returns, per price_class code in use by active elements, the largest max LOA / max beam
// and the largest length / width over berths, dynamic areas (max LOA and beam double as length and width) and
// land resources. Codes are sorted.
func PriceClassMaxDims(ctx context.Context, db bun.IDB, s MarinaScope) ([]PriceClassDims, error) {
	type row struct {
		Code string `bun:"code"`
		LOA  int64  `bun:"loa"`
		Beam int64  `bun:"beam"`
		Len  int64  `bun:"len"`
		Wid  int64  `bun:"wid"`
	}
	queries := []struct{ table, loa, beam, length, width string }{
		{"berths", "max_loa_cm", "max_beam_cm", "length_cm", "width_cm"},
		{"dynamic_areas", "max_loa_cm", "max_beam_cm", "max_loa_cm", "max_beam_cm"},
		{"land_resources", "length_cm", "width_cm", "length_cm", "width_cm"},
	}
	agg := map[string]*PriceClassDims{}
	var codes []string
	for _, q := range queries {
		var rows []row
		err := db.NewSelect().Table(q.table).ColumnExpr("price_class AS code").
			ColumnExpr("COALESCE(MAX("+q.loa+"), 0) AS loa").ColumnExpr("COALESCE(MAX("+q.beam+"), 0) AS beam").
			ColumnExpr("COALESCE(MAX("+q.length+"), 0) AS len").ColumnExpr("COALESCE(MAX("+q.width+"), 0) AS wid").
			Where("marina_id = ?", s.ID()).Where("active = ?", true).Where("price_class IS NOT NULL").Where("price_class <> ''").
			Group("price_class").Scan(ctx, &rows)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			d := agg[r.Code]
			if d == nil {
				d = &PriceClassDims{Code: r.Code}
				agg[r.Code] = d
				codes = append(codes, r.Code)
			}
			d.MaxLOACm, d.MaxBeamCm = max(d.MaxLOACm, r.LOA), max(d.MaxBeamCm, r.Beam)
			d.MaxLengthCm, d.MaxWidthCm = max(d.MaxLengthCm, r.Len), max(d.MaxWidthCm, r.Wid)
		}
	}
	slices.Sort(codes)
	out := make([]PriceClassDims, 0, len(codes))
	for _, c := range codes {
		out = append(out, *agg[c])
	}
	return out, nil
}
