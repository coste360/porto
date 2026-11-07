package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A minimal reader of the PDFs this package writes (fpdf output and its incremental updates): classic xref tables,
// direct /Length on streams, dictionaries as objects. It is not a general PDF parser.

// ErrMalformed means the bytes are not a PDF this package can read.
var ErrMalformed = errors.New("pdf: malformed document")

type document struct {
	raw       []byte
	offsets   map[int]int
	size      int
	root      int
	info      int
	startxref int
	id        []string // the two /ID strings (hex), when present
}

type object struct {
	num       int
	dict      string // the dictionary text "<<...>>" or the raw body of a non-dictionary object
	isDict    bool
	stream    []byte
	hasStream bool
}

var (
	reStartXref = regexp.MustCompile(`startxref\s+(\d+)\s+%%EOF\s*$`)
	reRef       = regexp.MustCompile(`(\d+) 0 R`)
	reID        = regexp.MustCompile(`/ID\s*\[\s*<([0-9A-Fa-f]*)>\s*<([0-9A-Fa-f]*)>\s*\]`)
)

func parse(raw []byte) (*document, error) {
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: no header", ErrMalformed)
	}
	m := reStartXref.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("%w: no startxref", ErrMalformed)
	}
	start, _ := strconv.Atoi(string(m[1]))
	d := &document{raw: raw, offsets: map[int]int{}, startxref: start}
	seen := map[int]bool{}
	first := true
	for off := start; ; {
		if seen[off] || off <= 0 || off >= len(raw) {
			return nil, fmt.Errorf("%w: bad xref offset", ErrMalformed)
		}
		seen[off] = true
		trailer, err := d.readXref(off)
		if err != nil {
			return nil, err
		}
		if first {
			first = false
			var ok bool
			if d.size, ok = dictInt(trailer, "/Size"); !ok {
				return nil, fmt.Errorf("%w: trailer without /Size", ErrMalformed)
			}
			if d.root, ok = dictRef(trailer, "/Root"); !ok {
				return nil, fmt.Errorf("%w: trailer without /Root", ErrMalformed)
			}
			d.info, _ = dictRef(trailer, "/Info")
			if id := reID.FindStringSubmatch(trailer); id != nil {
				d.id = []string{id[1], id[2]}
			}
		}
		prev, ok := dictInt(trailer, "/Prev")
		if !ok {
			break
		}
		off = prev
	}
	return d, nil
}

// readXref reads one xref section at off (entries already known are newer and kept) and returns its trailer.
func (d *document) readXref(off int) (string, error) {
	rest := d.raw[off:]
	if !bytes.HasPrefix(rest, []byte("xref")) {
		return "", fmt.Errorf("%w: no xref at %d", ErrMalformed, off)
	}
	end := bytes.Index(rest, []byte("trailer"))
	if end < 0 {
		return "", fmt.Errorf("%w: no trailer", ErrMalformed)
	}
	lines := strings.Split(strings.ReplaceAll(string(rest[4:end]), "\r", ""), "\n")
	num, count := 0, 0
	for _, ln := range lines {
		f := strings.Fields(ln)
		switch len(f) {
		case 0:
			continue
		case 2:
			num, _ = strconv.Atoi(f[0])
			count, _ = strconv.Atoi(f[1])
		case 3:
			if count <= 0 {
				return "", fmt.Errorf("%w: xref entry outside a subsection", ErrMalformed)
			}
			if f[2] == "n" {
				o, err := strconv.Atoi(f[0])
				if err != nil {
					return "", fmt.Errorf("%w: xref offset", ErrMalformed)
				}
				if _, ok := d.offsets[num]; !ok {
					d.offsets[num] = o
				}
			}
			num++
			count--
		default:
			return "", fmt.Errorf("%w: xref line", ErrMalformed)
		}
	}
	t := rest[end+len("trailer"):]
	i := bytes.Index(t, []byte("<<"))
	if i < 0 {
		return "", fmt.Errorf("%w: trailer dictionary", ErrMalformed)
	}
	j, err := dictEnd(t, i)
	if err != nil {
		return "", err
	}
	return string(t[i:j]), nil
}

// dictEnd returns the index after the ">>" closing the dictionary opened at b[i] ("<<").
func dictEnd(b []byte, i int) (int, error) {
	depth := 0
	for k := i; k < len(b); k++ {
		switch {
		case b[k] == '(':
			k = stringEnd(b, k)
		case b[k] == '<' && k+1 < len(b) && b[k+1] == '<':
			depth++
			k++
		case b[k] == '>' && k+1 < len(b) && b[k+1] == '>':
			depth--
			k++
			if depth == 0 {
				return k + 1, nil
			}
		case b[k] == '<':
			for k < len(b) && b[k] != '>' {
				k++
			}
		}
	}
	return 0, fmt.Errorf("%w: unterminated dictionary", ErrMalformed)
}

// stringEnd returns the index of the ")" closing the literal string opened at b[i].
func stringEnd(b []byte, i int) int {
	depth := 0
	for k := i; k < len(b); k++ {
		switch b[k] {
		case '\\':
			k++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(b) - 1
}

func (d *document) object(num int) (*object, error) {
	off, ok := d.offsets[num]
	if !ok || off < 0 || off >= len(d.raw) {
		return nil, fmt.Errorf("%w: object %d not in xref", ErrMalformed, num)
	}
	rest := d.raw[off:]
	head := []byte(strconv.Itoa(num) + " 0 obj")
	if !bytes.HasPrefix(rest, head) {
		return nil, fmt.Errorf("%w: object %d not at its offset", ErrMalformed, num)
	}
	p := len(head)
	for p < len(rest) && isSpace(rest[p]) {
		p++
	}
	o := &object{num: num}
	if bytes.HasPrefix(rest[p:], []byte("<<")) {
		e, err := dictEnd(rest, p)
		if err != nil {
			return nil, err
		}
		o.dict, o.isDict = string(rest[p:e]), true
		q := e
		for q < len(rest) && isSpace(rest[q]) {
			q++
		}
		if bytes.HasPrefix(rest[q:], []byte("stream")) {
			q += len("stream")
			if q < len(rest) && rest[q] == '\r' {
				q++
			}
			if q < len(rest) && rest[q] == '\n' {
				q++
			}
			n, ok := dictInt(o.dict, "/Length")
			if !ok || q+n > len(rest) {
				return nil, fmt.Errorf("%w: stream length of object %d", ErrMalformed, num)
			}
			o.stream, o.hasStream = rest[q:q+n], true
			if !bytes.Contains(rest[q+n:min(len(rest), q+n+32)], []byte("endstream")) {
				return nil, fmt.Errorf("%w: endstream of object %d", ErrMalformed, num)
			}
		}
		return o, nil
	}
	e := bytes.Index(rest[p:], []byte("endobj"))
	if e < 0 {
		return nil, fmt.Errorf("%w: endobj of object %d", ErrMalformed, num)
	}
	o.dict = strings.TrimSpace(string(rest[p : p+e]))
	return o, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

// topLevel returns the dictionary text with nested dictionaries, arrays' strings and string contents blanked,
// so key lookups only see the keys of the outer dictionary.
func topLevel(dict string) string {
	b := []byte(dict)
	out := make([]byte, len(b))
	copy(out, b)
	depth := 0
	for k := 0; k < len(b); k++ {
		switch {
		case b[k] == '(':
			e := stringEnd(b, k)
			for j := k; j <= e && j < len(out); j++ {
				out[j] = ' '
			}
			k = e
		case b[k] == '<' && k+1 < len(b) && b[k+1] == '<':
			depth++
			if depth > 1 {
				out[k], out[k+1] = ' ', ' '
			}
			k++
		case b[k] == '>' && k+1 < len(b) && b[k+1] == '>':
			if depth > 1 {
				out[k], out[k+1] = ' ', ' '
			}
			depth--
			k++
		default:
			if depth > 1 {
				out[k] = ' '
			}
		}
	}
	return string(out)
}

func dictInt(dict, key string) (int, bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(key) + `\s+(-?\d+)(\s|/|>|$)`)
	m := re.FindStringSubmatch(topLevel(dict))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

func dictRef(dict, key string) (int, bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(key) + `\s+(\d+)\s+0\s+R`)
	m := re.FindStringSubmatch(topLevel(dict))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// dictRefs returns the references of the array value of key.
func dictRefs(dict, key string) ([]int, bool) {
	tl := topLevel(dict)
	i := strings.Index(tl, key)
	if i < 0 {
		return nil, false
	}
	open := strings.Index(tl[i:], "[")
	if open < 0 {
		return nil, false
	}
	end := strings.Index(tl[i+open:], "]")
	if end < 0 {
		return nil, false
	}
	var out []int
	for _, m := range reRef.FindAllStringSubmatch(dict[i+open:i+open+end], -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out, true
}

// removeKey drops a top-level key and its value (a reference, a name, a number or an array) from a dictionary.
func removeKey(dict, key string) string {
	tl := topLevel(dict)
	i := strings.Index(tl, key+" ")
	if i < 0 {
		i = strings.Index(tl, key+"[")
	}
	if i < 0 {
		return dict
	}
	j := i + len(key)
	for j < len(tl) && tl[j] == ' ' {
		j++
	}
	switch {
	case j < len(tl) && tl[j] == '[':
		e := strings.Index(tl[j:], "]")
		j += e + 1
	default:
		if m := regexp.MustCompile(`^\d+\s+0\s+R`).FindStringIndex(tl[j:]); m != nil {
			j += m[1]
		} else {
			for j < len(tl) && tl[j] != '/' && tl[j] != '>' {
				j++
			}
		}
	}
	return dict[:i] + dict[j:]
}

// rewriteRefs maps the indirect references of a dictionary outside strings.
func rewriteRefs(dict string, mapNum func(int) int) string {
	b := []byte(dict)
	var out strings.Builder
	last := 0
	plainEnd := func(k int) {
		seg := string(b[last:k])
		out.WriteString(reRef.ReplaceAllStringFunc(seg, func(s string) string {
			n, _ := strconv.Atoi(reRef.FindStringSubmatch(s)[1])
			return strconv.Itoa(mapNum(n)) + " 0 R"
		}))
	}
	for k := 0; k < len(b); k++ {
		if b[k] == '(' {
			plainEnd(k)
			e := stringEnd(b, k)
			out.Write(b[k : e+1])
			k = e
			last = k + 1
		}
	}
	plainEnd(len(b))
	return out.String()
}

func (d *document) pagesObject() (*object, error) {
	cat, err := d.object(d.root)
	if err != nil {
		return nil, err
	}
	pn, ok := dictRef(cat.dict, "/Pages")
	if !ok {
		return nil, fmt.Errorf("%w: catalog without /Pages", ErrMalformed)
	}
	return d.object(pn)
}

// Verify checks the structure of a PDF written by this package: every xref entry of every revision points to its
// object, every object of the latest revision parses (streams by their /Length), and the page tree is consistent.
func Verify(raw []byte) error {
	d, err := parse(raw)
	if err != nil {
		return err
	}
	nums := make([]int, 0, len(d.offsets))
	for n := range d.offsets {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		if _, err := d.object(n); err != nil {
			return err
		}
		if n >= d.size {
			return fmt.Errorf("%w: object %d beyond /Size %d", ErrMalformed, n, d.size)
		}
	}
	pages, err := d.pagesObject()
	if err != nil {
		return err
	}
	kids, _ := dictRefs(pages.dict, "/Kids")
	count, _ := dictInt(pages.dict, "/Count")
	if len(kids) != count || count == 0 {
		return fmt.Errorf("%w: page tree has %d kids and /Count %d", ErrMalformed, len(kids), count)
	}
	for _, k := range kids {
		p, err := d.object(k)
		if err != nil {
			return err
		}
		if parent, _ := dictRef(p.dict, "/Parent"); parent != pages.num {
			return fmt.Errorf("%w: page %d has parent %d", ErrMalformed, k, parent)
		}
	}
	return nil
}
