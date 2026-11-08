package peripheral

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/pkg/ext"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update): %v", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file\n got: %q\nwant: %q", name, got, want)
	}
}

func sampleReceipt() desktopport.ReceiptDoc {
	return desktopport.ReceiptDoc{
		MarinaName: "Marina di Prova", LegalName: "Porto Prova S.r.l.", Address: "Via del Molo 1, 00100 Città",
		VATNumber: "01234567890", Number: "2026-000042", Date: "2026-11-08", Method: "cash", AmountCents: 123456,
		Lines: []desktopport.ReceiptLine{
			{Description: "Transito banchina A, posto A12, 2 notti con acqua ed elettricità", QtyMilli: 2000, GrossCents: 100000, VATRateBP: 2200},
			{Description: "Tassa di soggiorno", QtyMilli: 1000, GrossCents: 23456, VATRateBP: 0},
		},
		VAT: []desktopport.ReceiptVAT{
			{RateBP: 2200, NetCents: 81967, VATCents: 18033, GrossCents: 100000},
			{RateBP: 0, Nature: "N1", NetCents: 23456, GrossCents: 23456},
		},
		FiscalNumber: "20261108-0007", Footer: "Grazie e buon vento!", NotFiscal: true,
	}
}

func TestESCPOS_Golden(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
	}{{"receipt_80.escpos", 80}, {"receipt_58.escpos", 58}} {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderESCPOS(sampleReceipt(), tc.width)
			if !bytes.HasPrefix(got, []byte{0x1b, 0x40, 0x1b, 0x74, 0x13}) {
				t.Fatalf("missing init and CP858 selection: % x", got[:5])
			}
			if !bytes.HasSuffix(got, []byte{0x1d, 0x56, 0x42, 0x00}) {
				t.Fatal("missing partial cut")
			}
			if !bytes.Contains(got, []byte{0xd5, ' ', '1', '.', '2', '3', '4', ',', '5', '6'}) {
				t.Fatal("euro sign not encoded as CP858 0xD5")
			}
			cols := Columns(tc.width)
			for _, l := range bytes.Split(got, []byte{'\n'}) {
				text := bytes.TrimLeft(l, "\x1b\x1d@tEa\x00\x01\x13")
				if len(text) > cols+6 { // commands may prefix a line
					t.Fatalf("line longer than %d columns: %q", cols, text)
				}
			}
			golden(t, tc.name, got)
		})
	}
}

func TestESCPOS_NoCommandInjection(t *testing.T) {
	r := desktopport.ReceiptDoc{MarinaName: "A\x1b@\x1dVB", Number: "1"}
	got := RenderESCPOS(r, 80)
	if bytes.Contains(got[5:len(got)-4], []byte{0x1d, 0x56}) {
		t.Fatal("a control character of the text reached the printer")
	}
}

func TestZPL_Golden(t *testing.T) {
	got := RenderZPL(desktopport.LabelDoc{Lines: []string{"Porto", "Pontile ^B ~ 12", "Città"}, Copies: 2})
	if bytes.Count(got, []byte("^FD")) != 3 || bytes.Contains(got, []byte("^B")) || bytes.Contains(got, []byte("~")) {
		t.Fatalf("unexpected label: %q", got)
	}
	golden(t, "label.zpl", got)
}

func TestTransport_TargetValidation(t *testing.T) {
	ok := []string{
		"tcp://192.168.1.50", "tcp://192.168.1.50:9100", "tcp://10.0.0.2:9101", "tcp://127.0.0.1:9100",
		"tcp://printer.local", "tcp://stampante-cassa.local:9100", "tcp://[fd00::5]:9100", "cups://EPSON_TM_T20",
	}
	bad := []string{
		"", "http://192.168.1.5", "tcp://8.8.8.8:9100", "tcp://example.com:9100", "tcp://printer", "tcp://.local",
		"tcp://192.168.1.5:0", "tcp://192.168.1.5:70000", "tcp://user@192.168.1.5", "tcp://192.168.1.5/x",
		"tcp://169.254.1.1", "cups://bad queue", "cups://", "file:///tmp/x.bin", "tcp://192.168.1.5?x=1",
	}
	for _, s := range ok {
		if _, err := ParseTarget(s, TargetOptions{GOOS: "linux"}); err != nil {
			t.Errorf("%q refused: %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := ParseTarget(s, TargetOptions{GOOS: "linux"}); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("%q accepted", s)
		}
	}
	if _, err := ParseTarget("cups://q", TargetOptions{GOOS: "windows"}); err == nil {
		t.Error("cups accepted on windows")
	}
	if _, err := ParseTarget("file:///tmp/x.bin", TargetOptions{Dev: true}); err != nil {
		t.Errorf("file in dev refused: %v", err)
	}
	if _, err := ParseTarget("file://relative/x.bin", TargetOptions{Dev: true}); err == nil {
		t.Error("relative file accepted")
	}
}

func TestTransport_TCPAndCUPS(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(c)
		got <- buf.Bytes()
	}()
	tr, err := ParseTarget("tcp://"+ln.Addr().String(), TargetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Write(context.Background(), []byte("hello")); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-got:
		if string(b) != "hello" {
			t.Fatalf("got %q", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no bytes received")
	}

	var cmd []string
	var stdin []byte
	cups, err := ParseTarget("cups://Q1", TargetOptions{GOOS: "darwin", Exec: func(_ context.Context, in []byte, name string, args ...string) error {
		cmd, stdin = append([]string{name}, args...), in
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cups.Write(context.Background(), []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lp", "-d", "Q1", "-o", "raw"}; len(cmd) != 5 || cmd[2] != want[2] || cmd[4] != "raw" || len(stdin) != 2 {
		t.Fatalf("cups command %v stdin %v", cmd, stdin)
	}

	closed, _ := ParseTarget("tcp://127.0.0.1:1", TargetOptions{})
	if err := closed.Write(context.Background(), []byte("x")); !errors.Is(err, desktopport.ErrPeripheralUnavailable) {
		t.Fatalf("unreachable printer: %v", err)
	}
}

func TestFakeDrivers(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	p := Build(desktopport.PeripheralSettings{
		Receipt: desktopport.ReceiptPrinterSettings{Driver: "fake", Width: 58},
		Label:   desktopport.LabelPrinterSettings{Driver: "fake"},
		Reader:  desktopport.QRReaderSettings{Driver: "fake"},
		Fiscal:  desktopport.LocalFiscalSettings{Driver: "fake"},
	}, BuildOptions{FakeDir: dir})
	if p.Receipt == nil || p.Label == nil || p.Reader == nil || p.Fiscal == nil {
		t.Fatalf("drivers missing: %+v", p)
	}
	if err := p.Receipt.Print(ctx, sampleReceipt()); err != nil {
		t.Fatal(err)
	}
	if err := p.Label.Print(ctx, TestLabel()); err != nil {
		t.Fatal(err)
	}
	lines, _ := ReadFake(dir, desktopport.PeripheralReceipt)
	var rl FakeReceiptLine
	if len(lines) != 1 || json.Unmarshal(lines[0], &rl) != nil || rl.Number != "2026-000042" || rl.Width != 58 || rl.AmountCents != 123456 {
		t.Fatalf("receipt lines %s", lines)
	}
	if l, _ := ReadFake(dir, desktopport.PeripheralLabel); len(l) != 1 {
		t.Fatalf("label lines %d", len(l))
	}

	reader, ok := p.Reader.(*FakeReader)
	if !ok {
		t.Fatal("reader is not the fake")
	}
	reader.Push("PT1:abc")
	if s, err := reader.Next(ctx); err != nil || s != "PT1:abc" {
		t.Fatalf("scan %q %v", s, err)
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, err := reader.Next(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("empty reader: %v", err)
	}

	res, err := p.Fiscal.Issue(ctx, ext.FiscalDocumentRequest{DocumentID: "d1", Number: "20261108-0001", TotalCents: 500})
	if err != nil || res.ExternalRef != "FAKE-20261108-0001" {
		t.Fatalf("issue %+v %v", res, err)
	}
	off := FakeFiscal{Dir: dir, Mode: FakeFiscalOffline}
	_, err = off.Issue(ctx, ext.FiscalDocumentRequest{})
	var d interface{ Deferred() bool }
	if !errors.As(err, &d) || !d.Deferred() {
		t.Fatalf("offline fiscal must defer: %v", err)
	}
	if st, _ := off.Status(ctx); st.Online {
		t.Fatal("offline fiscal online")
	}
	if _, err := (FakeFiscal{Mode: FakeFiscalFail}).Issue(ctx, ext.FiscalDocumentRequest{}); !errors.Is(err, ErrFakeFiscalFailed) {
		t.Fatalf("fail mode: %v", err)
	}
}

func TestValidateAndNormalize(t *testing.T) {
	o := TargetOptions{GOOS: "linux"}
	if err := Validate(desktopport.PeripheralSettings{}, o); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	err := Validate(desktopport.PeripheralSettings{
		Receipt: desktopport.ReceiptPrinterSettings{Driver: "escpos", Target: "tcp://8.8.8.8", Width: 70},
		Label:   desktopport.LabelPrinterSettings{Driver: "laser"},
		Reader:  desktopport.QRReaderSettings{Driver: "serial"},
		Fiscal:  desktopport.LocalFiscalSettings{Driver: "fake", FakeMode: "maybe"},
	}, o)
	var se *desktopport.SettingsError
	if !errors.As(err, &se) || len(se.Fields) != 5 {
		t.Fatalf("got %v", err)
	}
	n := Normalize(desktopport.PeripheralSettings{Receipt: desktopport.ReceiptPrinterSettings{Driver: "fake", Target: "tcp://x"}})
	if n.Receipt.Target != "" || n.Receipt.Width != 80 || n.Reader.Driver != "keyboard" || n.Fiscal.Driver != "none" {
		t.Fatalf("normalize %+v", n)
	}
}

func TestFormat(t *testing.T) {
	for in, want := range map[int64]string{0: "0,00", 5: "0,05", 123456: "1.234,56", -1999: "-19,99", 100000000: "1.000.000,00"} {
		if got := FormatCents(in); got != want {
			t.Errorf("FormatCents(%d) = %q, want %q", in, got, want)
		}
	}
	if FormatRate(2200) != "22%" || FormatRate(550) != "5,5%" || FormatQty(2500) != "2,5" || FormatQty(1000) != "1" {
		t.Error("rate or quantity format")
	}
}
