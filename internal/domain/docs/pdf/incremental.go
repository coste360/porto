package pdf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
)

// newObj is an object of an incremental update.
type newObj struct {
	num    int
	dict   string
	stream []byte
	isDict bool
	hasStr bool
}

// writeUpdate appends an incremental update with objs to raw: the original bytes stay a byte-exact prefix.
func writeUpdate(raw []byte, d *document, objs []newObj, size int, id [2]string) []byte {
	var b bytes.Buffer
	b.Write(raw)
	if !bytes.HasSuffix(raw, []byte("\n")) {
		b.WriteByte('\n')
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].num < objs[j].num })
	offsets := map[int]int{}
	for _, o := range objs {
		offsets[o.num] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\n", o.num, o.dict)
		if o.hasStr {
			b.WriteString("stream\n")
			b.Write(o.stream)
			b.WriteString("\nendstream\n")
		}
		b.WriteString("endobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n")
	for i := 0; i < len(objs); {
		j := i
		for j+1 < len(objs) && objs[j+1].num == objs[j].num+1 {
			j++
		}
		fmt.Fprintf(&b, "%d %d\n", objs[i].num, j-i+1)
		for k := i; k <= j; k++ {
			fmt.Fprintf(&b, "%010d 00000 n \n", offsets[objs[k].num])
		}
		i = j + 1
	}
	b.WriteString("trailer\n<<\n")
	fmt.Fprintf(&b, "/Size %d\n/Root %d 0 R\n", size, d.root)
	if d.info > 0 {
		fmt.Fprintf(&b, "/Info %d 0 R\n", d.info)
	}
	fmt.Fprintf(&b, "/Prev %d\n/ID [<%s><%s>]\n>>\nstartxref\n%d\n%%%%EOF\n", d.startxref, id[0], id[1], xref)
	return b.Bytes()
}

// FileID is the 16-byte file identifier derived from the SHA-256 of data (hex).
func FileID(data []byte) string {
	h := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(h[:16]))
}

// ArchiveMeta are the metadata written in the XMP packet of a PDF/A document.
type ArchiveMeta struct {
	Title   string
	Author  string
	Lang    string
	Instant time.Time
}

// FinalizePDFA appends an incremental update that makes the document PDF/A-2b shaped (m4-plan §6.3, §15 D14): an
// XMP metadata stream (pdfaid part 2, conformance B), an /OutputIntents array with the own sRGB ICC profile, a new
// revision of the catalog and a trailer /ID of two copies of the SHA-256-derived file id. The original bytes are a
// prefix of the result.
func FinalizePDFA(raw []byte, m ArchiveMeta) ([]byte, error) {
	d, err := parse(raw)
	if err != nil {
		return nil, err
	}
	cat, err := d.object(d.root)
	if err != nil {
		return nil, err
	}
	xmpNum, iccNum, intentNum := d.size, d.size+1, d.size+2
	icc := SRGBProfile()
	xmp := xmpPacket(m)
	inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cat.dict), "<<"), ">>")
	inner = removeKey(removeKey(inner, "/Metadata"), "/OutputIntents")
	catalog := "<<" + strings.TrimRight(inner, " \n") + fmt.Sprintf("\n/Metadata %d 0 R\n/OutputIntents [%d 0 R]\n>>", xmpNum, intentNum)
	objs := []newObj{
		{num: d.root, dict: catalog, isDict: true},
		{num: xmpNum, dict: fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>", len(xmp)), stream: xmp, hasStr: true, isDict: true},
		{num: iccNum, dict: fmt.Sprintf("<< /N 3 /Length %d >>", len(icc)), stream: icc, hasStr: true, isDict: true},
		{num: intentNum, dict: fmt.Sprintf("<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB IEC61966-2.1) /Info (sRGB IEC61966-2.1) /DestOutputProfile %d 0 R >>", iccNum), isDict: true},
	}
	id := FileID(raw)
	return writeUpdate(raw, d, objs, intentNum+1, [2]string{id, id}), nil
}

func xmpPacket(m ArchiveMeta) []byte {
	at := m.Instant.UTC().Format("2006-01-02T15:04:05Z")
	lang := m.Lang
	if lang == "" {
		lang = "x-default"
	}
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"\ufeff\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/">` + "\n")
	b.WriteString("<pdfaid:part>2</pdfaid:part>\n<pdfaid:conformance>B</pdfaid:conformance>\n")
	b.WriteString(`<dc:format>application/pdf</dc:format>` + "\n")
	b.WriteString(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + esc(m.Title) + "</rdf:li></rdf:Alt></dc:title>\n")
	if m.Author != "" {
		b.WriteString("<dc:creator><rdf:Seq><rdf:li>" + esc(m.Author) + "</rdf:li></rdf:Seq></dc:creator>\n")
	}
	b.WriteString("<dc:language><rdf:Bag><rdf:li>" + esc(lang) + "</rdf:li></rdf:Bag></dc:language>\n")
	b.WriteString("<xmp:CreateDate>" + at + "</xmp:CreateDate>\n<xmp:ModifyDate>" + at + "</xmp:ModifyDate>\n")
	b.WriteString("<xmp:MetadataDate>" + at + "</xmp:MetadataDate>\n<xmp:CreatorTool>" + Producer + "</xmp:CreatorTool>\n")
	b.WriteString("<pdf:Producer>" + Producer + "</pdf:Producer>\n")
	b.WriteString("</rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n<?xpacket end=\"r\"?>")
	return []byte(b.String())
}

// AppendPages appends the pages of extra (a PDF written by Draw) to original by incremental update: the objects of
// extra are renumbered after the original /Size, its pages are added to the original page tree and the original
// bytes stay a byte-exact prefix of the result (the OTP evidence page and the paper cover page, m4-plan §6.3).
func AppendPages(original, extra []byte) ([]byte, error) {
	o, err := parse(original)
	if err != nil {
		return nil, fmt.Errorf("pdf: original: %w", err)
	}
	x, err := parse(extra)
	if err != nil {
		return nil, fmt.Errorf("pdf: extra: %w", err)
	}
	oPages, err := o.pagesObject()
	if err != nil {
		return nil, err
	}
	xPages, err := x.pagesObject()
	if err != nil {
		return nil, err
	}
	xKids, ok := dictRefs(xPages.dict, "/Kids")
	if !ok || len(xKids) == 0 {
		return nil, fmt.Errorf("%w: extra has no pages", ErrMalformed)
	}
	shift := o.size - 1
	mapNum := func(n int) int {
		if n == xPages.num {
			return oPages.num
		}
		return n + shift
	}
	skip := map[int]bool{x.root: true, x.info: true, xPages.num: true}
	var objs []newObj
	maxNum := o.size - 1
	nums := make([]int, 0, len(x.offsets))
	for n := range x.offsets {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		if skip[n] {
			continue
		}
		obj, err := x.object(n)
		if err != nil {
			return nil, err
		}
		nn := mapNum(n)
		maxNum = max(maxNum, nn)
		objs = append(objs, newObj{
			num: nn, dict: rewriteRefs(obj.dict, mapNum), stream: obj.stream, hasStr: obj.hasStream, isDict: obj.isDict,
		})
	}
	oKids, _ := dictRefs(oPages.dict, "/Kids")
	count, _ := dictInt(oPages.dict, "/Count")
	kids := make([]string, 0, len(oKids)+len(xKids))
	for _, k := range oKids {
		kids = append(kids, strconv.Itoa(k)+" 0 R")
	}
	for _, k := range xKids {
		kids = append(kids, strconv.Itoa(mapNum(k))+" 0 R")
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(oPages.dict), "<<"), ">>")
	inner = removeKey(removeKey(inner, "/Kids"), "/Count")
	pages := "<<" + strings.TrimRight(inner, " \n") + "\n/Kids [" + strings.Join(kids, " ") + "]\n/Count " +
		strconv.Itoa(count+len(xKids)) + "\n>>"
	objs = append(objs, newObj{num: oPages.num, dict: pages, isDict: true})
	first := FileID(original)
	if len(o.id) == 2 && o.id[0] != "" {
		first = o.id[0]
	}
	second := FileID(append(append([]byte{}, original...), extra...))
	return writeUpdate(original, o, objs, maxNum+1, [2]string{first, second}), nil
}
