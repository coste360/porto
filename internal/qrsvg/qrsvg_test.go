package qrsvg

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	svg, err := Render("porto://pair?v=1&url=http%3A%2F%2Flocalhost%3A8080&code=ABCD-EFGH")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`) || !strings.HasSuffix(svg, "</svg>") ||
		strings.Contains(svg, "<script") || !strings.Contains(svg, `d="M`) {
		t.Fatalf("svg: %.200s", svg)
	}
	again, _ := Render("porto://pair?v=1&url=http%3A%2F%2Flocalhost%3A8080&code=ABCD-EFGH")
	if again != svg {
		t.Fatal("not deterministic")
	}
}
