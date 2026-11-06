// Command genvectors writes the Footprint and AreaOf parity vectors (m1-plan §2.8):
//
//	go run ./internal/domain/layout/internal/genvectors -seed 1 -n 400 [-out testdata/footprint]
//
// cases.json holds the hand-written edge cases followed by n seeded random cases; area-cells.json the AreaOf
// cases. The output is deterministic: TestFootprint_VectorsUpToDate fails when the committed files drift.
package main

import (
	"flag"
	"log"
	"os" //nolint:depguard // code generator, not part of the pure package: it writes the vector files
	"path/filepath"

	"github.com/coste360/porto/internal/domain/layout/internal/vectors"
)

func main() {
	seed := flag.Uint64("seed", 1, "random seed")
	n := flag.Int("n", 400, "number of random cases")
	out := flag.String("out", "testdata/footprint", "output directory")
	flag.Parse()
	cases, err := vectors.Encode(vectors.Cases(*seed, *n))
	if err != nil {
		log.Fatal(err)
	}
	areas, err := vectors.EncodeAreas(vectors.AreaCases())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil { //nolint:gosec // repository directory
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "cases.json"), cases, 0o644); err != nil { //nolint:gosec // committed test data
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "area-cells.json"), areas, 0o644); err != nil { //nolint:gosec // committed test data
		log.Fatal(err)
	}
}
