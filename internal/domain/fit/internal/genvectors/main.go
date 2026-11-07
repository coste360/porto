// Command genvectors writes the fit parity vectors (m3-plan §2.1):
//
//	go run ./internal/domain/fit/internal/genvectors -seed 1 -n 300 [-out testdata/fit]
//
// cases.json holds the hand-written edge cases followed by n seeded random cases. The output is deterministic:
// TestFit_VectorsUpToDate fails when the committed file drifts.
package main

import (
	"flag"
	"log"
	"os" //nolint:depguard // code generator, not part of the pure package: it writes the vector file
	"path/filepath"

	"github.com/coste360/porto/internal/domain/fit/internal/vectors"
)

func main() {
	seed := flag.Uint64("seed", 1, "random seed")
	n := flag.Int("n", 300, "number of random cases")
	out := flag.String("out", "testdata/fit", "output directory")
	flag.Parse()
	b, err := vectors.Encode(vectors.Cases(*seed, *n))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil { //nolint:gosec // repository directory
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "cases.json"), b, 0o644); err != nil { //nolint:gosec // committed test data
		log.Fatal(err)
	}
}
