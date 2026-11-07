package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// kitDir is the brand kit folder the desktop icons come from (ADR 0007).
const kitDir = "../../../assets/brand/porto/05_PNG_Icone"

// TestBrandIcons_MatchKit checks that the Windows icon carries the kit PNGs byte for byte and that every icon the
// packaging scripts use exists in the kit (they are copied, never redrawn).
func TestBrandIcons_MatchKit(t *testing.T) {
	var kit [][]byte
	for _, size := range []string{"64", "128", "256", "512", "1024"} {
		b, err := os.ReadFile(filepath.Join(kitDir, "porto_app_navy_"+size+".png"))
		if err != nil {
			t.Fatalf("kit icon %s: %v", size, err)
		}
		kit = append(kit, b)
	}
	var buf bytes.Buffer
	if err := Write(&buf, kit[:3]); err != nil { // .ico: 64, 128, 256
		t.Fatal(err)
	}
	got, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d entries", len(got))
	}
	for i := range got {
		if !bytes.Equal(got[i], kit[i]) {
			t.Fatalf("entry %d differs from the kit", i)
		}
	}
	if err := Write(&buf, kit[3:4]); err == nil {
		t.Fatal("512 px accepted in an .ico")
	}
}
