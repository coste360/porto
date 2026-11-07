package desktop

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return string(b)
}

func TestLogWriter_KeepsRecordsWholeAndBounded(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, 64, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 30 records of 16 bytes, 4 per file: records 20-27 in the two backups, 28-29 in the live file.
	for i := range 30 {
		rec := []byte(strings.Repeat(string(rune('a'+i%26)), 15) + "\n")
		if n, err := w.Write(rec); err != nil || n != len(rec) {
			t.Fatalf("write %d: n=%d err=%v", i, n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, LogFileName)
	var all string
	for _, name := range []string{live + ".2", live + ".1", live} {
		got := readLog(t, name)
		if len(got) > 64 {
			t.Errorf("%s holds %d bytes, limit 64", filepath.Base(name), len(got))
		}
		for _, line := range strings.SplitAfter(got, "\n") {
			if line != "" && len(line) != 16 {
				t.Errorf("%s has a split record %q", filepath.Base(name), line)
			}
		}
		info, _ := os.Stat(name)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", filepath.Base(name), info.Mode().Perm())
		}
		all += got
	}
	if _, err := os.Stat(live + ".3"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a fourth file was kept")
	}
	if len(all) != 10*16 || all[len(all)-2] != byte('a'+29%26) {
		t.Errorf("unexpected surviving content %q", all)
	}
}

func TestLogWriter_AppendsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("first-123456\n")) // 13 bytes
	_ = w.Close()
	w, err = OpenLogWriter(dir, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	// The reopened writer knows the file already holds 13 bytes: another 13 must go to a new file.
	_, _ = w.Write([]byte("second-12345\n"))
	_ = w.Close()
	live := filepath.Join(dir, LogFileName)
	if got := readLog(t, live+".1"); got != "first-123456\n" {
		t.Errorf("backup %q", got)
	}
	if got := readLog(t, live); got != "second-12345\n" {
		t.Errorf("live %q", got)
	}
}

func TestLogWriter_OversizedRecordAndSingleFile(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("z", 30) + "\n"
	for range 3 {
		if _, err := w.Write([]byte(big)); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || readLog(t, filepath.Join(dir, LogFileName)) != big {
		t.Fatalf("keep=1 must leave one file with the last record, got %d entries", len(ents))
	}
}

func TestLogWriter_ClosedAndConcurrent(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.limit != 10<<20 || w.keep != 5 {
		t.Fatalf("defaults %d x %d", w.limit, w.keep)
	}
	w.limit = 500
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Write([]byte("0123456789\n"))
		}()
	}
	wg.Wait()
	_ = w.Close()
	if _, err := w.Write([]byte("late\n")); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	total := 0
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		total += len(readLog(t, filepath.Join(dir, e.Name())))
	}
	if total != 1100 || len(ents) != 3 {
		t.Fatalf("%d bytes in %d files, want 1100 in 3", total, len(ents))
	}
}
