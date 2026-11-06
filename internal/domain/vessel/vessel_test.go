package vessel

import (
	"testing"

	"github.com/coste360/porto/internal/store"
)

func TestVessel_UnitClass(t *testing.T) {
	for _, c := range []struct {
		loa  int64
		th   [2]int64
		want string
	}{
		{999, DefaultUnitThresholdsCm, UnitNatante},
		{1000, DefaultUnitThresholdsCm, UnitImbarcazione},
		{2400, DefaultUnitThresholdsCm, UnitImbarcazione},
		{2401, DefaultUnitThresholdsCm, UnitNave},
		{1100, [2]int64{1200, 3000}, UnitNatante},
	} {
		if got := UnitClass(c.loa, c.th); got != c.want {
			t.Errorf("UnitClass(%d, %v) = %s, want %s", c.loa, c.th, got, c.want)
		}
	}
}

func TestVessel_EffectiveDimensions(t *testing.T) {
	v := &store.Vessel{LOACm: 1200, BeamCm: 400, DraftCm: 150, MeasuredLOACm: 1260, MeasuredBeamCm: 410, DimensionsSource: "declared"}
	if l, b, d := Effective(v); l != 1200 || b != 400 || d != 150 {
		t.Fatalf("declared %d %d %d", l, b, d)
	}
	v.DimensionsSource = "measured"
	if l, b, d := Effective(v); l != 1260 || b != 410 || d != 150 {
		t.Fatalf("measured %d %d %d", l, b, d)
	}
}

func TestVessel_ApplyValidation(t *testing.T) {
	name, loa, beam, bad := "Aurora", int64(1200), int64(400), int64(-1)
	row := &store.Vessel{}
	if _, err := apply(row, Patch{Name: &name, LOACm: &loa, BeamCm: &beam}, true); err != nil {
		t.Fatal(err)
	}
	if row.HullType != "other" || row.DimensionsSource != "declared" || row.SearchText != "aurora" {
		t.Fatalf("defaults %+v", row)
	}
	measured := "measured"
	if _, err := apply(row, Patch{DimensionsSource: &measured}, false); err == nil {
		t.Fatal("measured source without measurements accepted")
	}
	if _, err := apply(row, Patch{DraftCm: &bad}, false); err == nil {
		t.Fatal("negative draft accepted")
	}
	amps := int64(40)
	if _, err := apply(row, Patch{PowerRequiredA: &amps}, false); err == nil {
		t.Fatal("40 A accepted")
	}
}
