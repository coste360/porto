package watch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSensor_NoOccupancyWrites is the grep half of INV-100: no file of internal/domain/sensor/**, nor the sensor
// code that lives elsewhere (internal/domain/presence/sensor.go, internal/domain/maintenance/sensor.go,
// internal/store/sensor_*.go), calls a store write on allocations, occupancy rows, releases, contracts or charges,
// nor the occupancy write helpers. The confirm actions
// reach those tables only through the domain services (occupancy.CreateRelease, occupancy.RevokeRelease,
// presence.Record) with the user's principal.
func TestSensor_NoOccupancyWrites(t *testing.T) {
	forbidden := regexp.MustCompile(`store\.(Insert|Update|Delete|Upsert|Lock)\w*(Allocation|Occupanc|Release|Contract|Charge)\w*\(|` +
		`\.(AllocateTx|ConvertTx|TruncateTx|CancelTx|ResizeTx|MoveTx|LayoutTx|LayoutPricedTx|AllocateOwnerTx|RemoveOwnerNightsTx|OnArrivalTx|OnDepartureTx)\(|` +
		`NewInsert\(\)\.Model\(&?(store\.)?(Allocation|BerthOccupancy|CellOccupancy|LandOccupancy|Release|Contract|Charge)\b|` +
		`(?i)(INSERT INTO|UPDATE|DELETE FROM)\s+(allocations|berth_occupancies|cell_occupancies|land_occupancies|releases|contracts|charges)\b`)
	scan := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if forbidden.MatchString(line) {
				t.Errorf("%s:%d writes occupancy or contracts: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	}
	root := filepath.Clean("..")
	n := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n++
		scan(path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("only %d files scanned: wrong root", n)
	}
	others, err := filepath.Glob(filepath.Join("..", "..", "..", "store", "sensor_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	others = append(others, filepath.Join("..", "..", "presence", "sensor.go"), filepath.Join("..", "..", "maintenance", "sensor.go"))
	stores := 0
	for _, p := range others {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		if strings.Contains(p, "store") {
			stores++
		}
		scan(p)
	}
	if stores < 5 {
		t.Fatalf("only %d store sensor files scanned: wrong path", stores)
	}
}
