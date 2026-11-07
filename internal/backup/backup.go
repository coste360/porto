// Package backup writes and restores portable backups: a gzip tar with manifest.json, one JSONL file per
// table and the encrypted blobs of the file store under files/ (copied as they are, still encrypted). The format does not depend on the dialect, so a backup taken on SQLite restores on PostgreSQL and the
// other way round (spec 31.1).
package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/store"
)

// Format identifies the backup format.
const Format = "porto-backup/1"

const manifestName = "manifest.json"

// Errors.
var (
	ErrFormat         = errors.New("backup: not a Porto backup")
	ErrSchemaMismatch = errors.New("backup: schema version differs from the database")
	ErrNotEmpty       = errors.New("backup: the database is not empty (use --force to wipe it)")
	ErrCorrupt        = errors.New("backup: archive is corrupt")
	ErrNoFilesDir     = errors.New("backup: the archive contains files but no files directory was given")
)

// filesPrefix is the archive directory of the file store blobs: files/<storage_key>.
const filesPrefix = "files/"

// deferredColumns are self-referencing foreign keys: restore inserts them as NULL and sets them once the whole
// table is loaded, because a row may point to a row with a later id.
var deferredColumns = map[string][]string{
	"harbor_structures": {"parent_id"},
	"customers":         {"merged_into_id"},
	"revenue_classes":   {"parent_id"},
	"price_lists":       {"source_price_list_id"},
	"allocations":       {"moved_from_id"},
}

// lateColumns are inserted with a placeholder value and set to their real value after every table is loaded.
// price_lists.status: the immutability triggers (INV-22) refuse periods, bands and rules of a list that is not a
// draft, so lists are restored as drafts and get their status once every table (rules, item snapshot) is in.
// inquiries and quotes (M3) point forward to quotes, quote options and contracts: they are inserted as NULL and
// set at the end.
// M4: active millesimal tables and issued fee and common charge runs freeze their entries, lines and allocations
// (INV-41, INV-44), so they are restored as drafts and get their status at the end, like price lists.
// M5: exported charges freeze their lines and cancelled charges every column (INV-51), so charges are restored as
// to_invoice and open and get their statuses at the end.
var lateColumns = map[string]map[string]any{
	"price_lists":         {"status": "draft"},
	"inquiries":           {"quote_id": nil, "contract_id": nil},
	"quotes":              {"accepted_option_id": nil, "contract_id": nil},
	"millesimal_tables":   {"status": "draft"},
	"management_fee_runs": {"status": "draft"},
	"common_charge_runs":  {"status": "draft"},
	"charges":             {"invoicing_status": "to_invoice", "payment_status": "open"},
}

// lateUpdate is a column value set after the whole archive is loaded.
type lateUpdate struct {
	table, id, col string
	value          any
}

// File describes one blob of the file store in the archive.
type File struct {
	Path   string `json:"path"` // files/<storage_key>
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Table describes one table in the archive.
type Table struct {
	Name    string         `json:"name"`
	File    string         `json:"file"`
	Rows    int64          `json:"rows"`
	SHA256  string         `json:"sha256"`
	Columns []store.Column `json:"columns"`
}

// Manifest describes an archive.
type Manifest struct {
	Format        string    `json:"format"`
	PortoVersion  string    `json:"porto_version"`
	SchemaVersion int64     `json:"schema_version"`
	Dialect       string    `json:"dialect"`
	CreatedAt     time.Time `json:"created_at"`
	Tables        []Table   `json:"tables"`
	Files         []File    `json:"files,omitempty"`
}

// Rows returns the row count of a table in the manifest.
func (m *Manifest) Rows(table string) int64 {
	for _, t := range m.Tables {
		if t.Name == table {
			return t.Rows
		}
	}
	return 0
}

// Backup writes a consistent snapshot of every table of store.BackupTables to out, plus every blob under
// filesDir (the file store directory, PORTO_DATA_DIR/files; empty = no files).
func Backup(ctx context.Context, db *store.DB, out io.Writer, version, filesDir string) (*Manifest, error) {
	mig, err := db.Migrator()
	if err != nil {
		return nil, err
	}
	schema, err := mig.Version(ctx)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "porto-backup-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	m := &Manifest{
		Format: Format, PortoVersion: version, SchemaVersion: schema, Dialect: string(db.Kind),
		CreatedAt: clock.System{}.Now().Truncate(time.Second),
	}
	err = db.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		for _, name := range store.BackupTables() {
			t, err := dumpTable(ctx, tx, db.Kind, name, tmp)
			if err != nil {
				return err
			}
			m.Tables = append(m.Tables, *t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if m.Files, err = listFiles(filesDir); err != nil {
		return nil, err
	}
	if err := writeArchive(out, m, tmp, filesDir); err != nil {
		return nil, err
	}
	return m, nil
}

func dumpTable(ctx context.Context, tx bun.IDB, d store.Dialect, name, dir string) (*Table, error) {
	cols, err := store.TableColumns(ctx, tx, d, name)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(dir, name+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sum := sha256.New()
	w := bufio.NewWriter(io.MultiWriter(f, sum))
	t := &Table{Name: name, File: "tables/" + name + ".jsonl", Columns: cols}
	err = store.DumpTable(ctx, tx, name, cols, func(row []any) error {
		obj := make(map[string]any, len(cols))
		for i, c := range cols {
			obj[c.Name] = row[i]
		}
		b, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		t.Rows++
		if _, err := w.Write(b); err != nil {
			return err
		}
		return w.WriteByte('\n')
	})
	if err != nil {
		return nil, fmt.Errorf("backup: %s: %w", name, err)
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	t.SHA256 = hex.EncodeToString(sum.Sum(nil))
	return t, nil
}

// listFiles returns the blobs under dir (regular files, hidden temporary files skipped) with their checksums.
func listFiles(dir string) ([]File, error) {
	if dir == "" {
		return nil, nil
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("backup: files: %w", err)
	}
	defer root.Close()
	var files []File
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		f, err := root.Open(rel)
		if err != nil {
			return err
		}
		defer f.Close()
		sum := sha256.New()
		n, err := io.Copy(sum, f)
		if err != nil {
			return err
		}
		files = append(files, File{Path: filesPrefix + rel, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("backup: files: %w", err)
	}
	return files, nil
}

// validFilePath reports whether an archive file path is files/<relative path> without traversal.
func validFilePath(p string) bool {
	if !strings.HasPrefix(p, filesPrefix) || path.Clean(p) != p {
		return false
	}
	rel := strings.TrimPrefix(p, filesPrefix)
	return rel != "" && !path.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, "../") && !strings.Contains(rel, "/../")
}

func writeArchive(out io.Writer, m *Manifest, dir, filesDir string) error {
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeEntry(tw, manifestName, bytes.NewReader(mb), int64(len(mb)), m.CreatedAt); err != nil {
		return err
	}
	for _, t := range m.Tables {
		if err := writeFileEntry(tw, t.File, filepath.Join(dir, t.Name+".jsonl"), m.CreatedAt); err != nil {
			return err
		}
	}
	for _, f := range m.Files {
		src := filepath.Join(filesDir, filepath.FromSlash(strings.TrimPrefix(f.Path, filesPrefix)))
		if err := writeFileEntry(tw, f.Path, src, m.CreatedAt); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeFileEntry(tw *tar.Writer, name, src string, at time.Time) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return writeEntry(tw, name, f, st.Size(), at)
}

func writeEntry(tw *tar.Writer, name string, r io.Reader, size int64, at time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: at, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.Copy(tw, r)
	return err
}

// Restore loads an archive into db. The database must be migrated to the archive's schema version and empty;
// with force it is reset (every migration rolled back and applied again) first. Rows are inserted in one
// transaction, in foreign key order. The archive's blobs are copied into filesDir (the file store directory)
// after the rows are committed.
func Restore(ctx context.Context, db *store.DB, in io.Reader, force bool, filesDir string) (*Manifest, error) {
	tmp, err := os.MkdirTemp("", "porto-restore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	m, err := extract(in, tmp)
	if err != nil {
		return nil, err
	}
	if len(m.Files) > 0 && filesDir == "" {
		return nil, ErrNoFilesDir
	}
	mig, err := db.Migrator()
	if err != nil {
		return nil, err
	}
	if force {
		if err := mig.Reset(ctx); err != nil {
			return nil, err
		}
		if _, err := mig.Up(ctx); err != nil {
			return nil, err
		}
	}
	schema, err := mig.Version(ctx)
	if err != nil {
		return nil, err
	}
	if schema != m.SchemaVersion {
		return nil, fmt.Errorf("%w: archive %d, database %d", ErrSchemaMismatch, m.SchemaVersion, schema)
	}
	for _, name := range append(store.BackupTables(), "sessions", "idempotency_keys") {
		n, err := store.CountRows(ctx, db.DB, name)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, ErrNotEmpty
		}
	}
	err = db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var late []lateUpdate
		for _, t := range m.Tables {
			if err := loadTable(ctx, tx, db.Kind, t, filepath.Join(tmp, t.Name+".jsonl"), &late); err != nil {
				return fmt.Errorf("backup: restore %s: %w", t.Name, err)
			}
		}
		for _, u := range late {
			if err := store.SetColumnByID(ctx, tx, u.table, u.col, u.id, u.value); err != nil {
				return fmt.Errorf("backup: restore %s.%s: %w", u.table, u.col, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := restoreFiles(m, filepath.Join(tmp, "files"), filesDir); err != nil {
		return nil, err
	}
	return m, nil
}

// restoreFiles copies the extracted blobs into filesDir (created 0700), replacing files with the same key. Both
// sides go through os.Root, so a path can never leave its directory.
func restoreFiles(m *Manifest, from, filesDir string) error {
	if len(m.Files) == 0 {
		return nil
	}
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	src, err := os.OpenRoot(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(filesDir)
	if err != nil {
		return err
	}
	defer dst.Close()
	for _, f := range m.Files {
		rel := strings.TrimPrefix(f.Path, filesPrefix)
		if dir := path.Dir(rel); dir != "." {
			if err := dst.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
		b, err := src.ReadFile(rel)
		if err != nil {
			return err
		}
		if err := dst.WriteFile(rel, b, 0o600); err != nil {
			return fmt.Errorf("backup: restore file: %w", err)
		}
	}
	return nil
}

// extract reads the archive into dir, checking the manifest, the file list and every checksum.
func extract(in io.Reader, dir string) (*Manifest, error) {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, ErrFormat
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != manifestName {
		return nil, ErrFormat
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil || m.Format != Format {
		return nil, ErrFormat
	}
	want := map[string]Table{}
	known := map[string]bool{}
	for _, n := range store.BackupTables() {
		known[n] = true
	}
	for _, t := range m.Tables {
		if !known[t.Name] || t.File != "tables/"+t.Name+".jsonl" {
			return nil, fmt.Errorf("%w: unexpected table %q", ErrCorrupt, t.Name)
		}
		want[t.File] = t
	}
	wantFiles := map[string]File{}
	for _, f := range m.Files {
		if !validFilePath(f.Path) {
			return nil, fmt.Errorf("%w: unexpected file %q", ErrCorrupt, f.Path)
		}
		wantFiles[f.Path] = f
	}
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err) //nolint:errorlint // one wrapped sentinel is enough
		}
		name := path.Clean(hdr.Name)
		if f, ok := wantFiles[name]; ok {
			if seen[name] {
				return nil, fmt.Errorf("%w: duplicate entry %q", ErrCorrupt, hdr.Name)
			}
			seen[name] = true
			dst := filepath.Join(dir, "files", filepath.FromSlash(strings.TrimPrefix(name, filesPrefix)))
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return nil, err
			}
			if err := extractFile(tr, dst, f.SHA256); err != nil {
				return nil, err
			}
			continue
		}
		t, ok := want[name]
		if !ok || seen[t.File] {
			return nil, fmt.Errorf("%w: unexpected entry %q", ErrCorrupt, hdr.Name)
		}
		seen[t.File] = true
		if err := extractFile(tr, filepath.Join(dir, t.Name+".jsonl"), t.SHA256); err != nil {
			return nil, err
		}
	}
	if len(seen) != len(want)+len(wantFiles) {
		return nil, fmt.Errorf("%w: missing table files", ErrCorrupt)
	}
	return &m, nil
}

func extractFile(r io.Reader, dst, wantSum string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, sum), r); err != nil {
		return err
	}
	if hex.EncodeToString(sum.Sum(nil)) != wantSum {
		return fmt.Errorf("%w: checksum of %s", ErrCorrupt, filepath.Base(dst))
	}
	return nil
}

func loadTable(ctx context.Context, tx bun.IDB, d store.Dialect, t Table, src string, late *[]lateUpdate) error {
	cols, err := store.TableColumns(ctx, tx, d, t.Name)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	names := make([]string, len(cols))
	deferred := map[int]bool{}
	for i, c := range cols {
		names[i] = c.Name
		for _, d := range deferredColumns[t.Name] {
			if d == c.Name {
				deferred[i] = true
			}
		}
	}
	type later struct {
		id, col string
		value   any
	}
	var updates []later
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var rows int64
	for sc.Scan() {
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return fmt.Errorf("%w: line %d", ErrCorrupt, rows+1)
		}
		args := make([]any, len(cols))
		for i, c := range cols {
			v, present := obj[c.Name]
			if !present {
				return fmt.Errorf("%w: column %s missing", ErrCorrupt, c.Name)
			}
			if c.Kind == store.KindBytes && v != nil {
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("%w: column %s", ErrCorrupt, c.Name)
				}
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return fmt.Errorf("%w: column %s", ErrCorrupt, c.Name)
				}
				v = b
			}
			a, err := store.ImportValue(c.Kind, v)
			if err != nil {
				return fmt.Errorf("column %s: %w", c.Name, err)
			}
			if deferred[i] && a != nil {
				id, _ := obj["id"].(string)
				updates = append(updates, later{id: id, col: c.Name, value: a})
				a = nil
			}
			if placeholder, ok := lateColumns[t.Name][c.Name]; ok && a != nil {
				id, _ := obj["id"].(string)
				if a != placeholder {
					*late = append(*late, lateUpdate{table: t.Name, id: id, col: c.Name, value: a})
				}
				a = placeholder
			}
			args[i] = a
		}
		if err := store.InsertRow(ctx, tx, t.Name, names, args); err != nil {
			return err
		}
		rows++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if rows != t.Rows {
		return fmt.Errorf("%w: %d rows, manifest says %d", ErrCorrupt, rows, t.Rows)
	}
	for _, u := range updates {
		if err := store.SetColumnByID(ctx, tx, t.Name, u.col, u.id, u.value); err != nil {
			return err
		}
	}
	return nil
}
