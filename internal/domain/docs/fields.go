package docs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs/tmpl"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// The field catalogue of a kind is derived by reflection from its view struct (docs/views, doc tag
// "name,description_key"); the same walk converts a view into the value tree templates are evaluated on, so
// validation, documentation and data can never disagree (m4-plan §6.1, §15 D20).

var (
	typeDate   = reflect.TypeOf(calendar.Date{})
	typeTime   = reflect.TypeOf(time.Time{})
	typeCents  = reflect.TypeOf(money.Cents(0))
	typeCm     = reflect.TypeOf(views.Cm(0))
	typeBP     = reflect.TypeOf(views.BP(0))
	typeMillis = reflect.TypeOf(views.Millis(0))
	typeNotes  = reflect.TypeOf(views.Notes(""))
)

func fieldType(t reflect.Type) tmpl.Type {
	switch t {
	case typeDate:
		return tmpl.TDate
	case typeTime:
		return tmpl.TDateTime
	case typeCents:
		return tmpl.TCents
	case typeCm:
		return tmpl.TCm
	case typeBP:
		return tmpl.TBP
	case typeMillis:
		return tmpl.TMillis
	case typeNotes:
		return tmpl.TNotes
	}
	switch t.Kind() {
	case reflect.Pointer:
		return fieldType(t.Elem())
	case reflect.Struct:
		return tmpl.TObject
	case reflect.Slice:
		return tmpl.TList
	case reflect.Bool:
		return tmpl.TBool
	case reflect.Int, reflect.Int64, reflect.Int32:
		return tmpl.TInt
	default:
		return tmpl.TText
	}
}

func docTag(f reflect.StructField) (name, key string, ok bool) {
	tag := f.Tag.Get("doc")
	if tag == "" {
		return "", "", false
	}
	name, key, _ = strings.Cut(tag, ",")
	return name, key, name != ""
}

func walkFields(t reflect.Type, prefix string, out *[]tmpl.Field) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := range t.NumField() {
		f := t.Field(i)
		name, key, ok := docTag(f)
		if !ok {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := fieldType(f.Type)
		*out = append(*out, tmpl.Field{Path: path, Type: ft, List: ft == tmpl.TList, DescriptionKey: key})
		switch ft {
		case tmpl.TObject:
			walkFields(f.Type, path, out)
		case tmpl.TList:
			el := f.Type.Elem()
			for el.Kind() == reflect.Pointer {
				el = el.Elem()
			}
			if el.Kind() == reflect.Struct && el != typeDate && el != typeTime {
				walkFields(el, path, out)
			}
		}
	}
}

var (
	fieldsMu    sync.Mutex
	fieldsCache = map[Kind][]tmpl.Field{}
)

// Fields returns the field catalogue of a kind (nil for kinds without a view), sorted by path.
func Fields(kind Kind) []tmpl.Field {
	fieldsMu.Lock()
	defer fieldsMu.Unlock()
	if f, ok := fieldsCache[kind]; ok {
		return f
	}
	info, ok := KindInfoOf(kind)
	if !ok || info.View == nil {
		return nil
	}
	var out []tmpl.Field
	walkFields(info.View, "", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	fieldsCache[kind] = out
	return out
}

// Schema returns the catalogue of a kind as a validation schema.
func Schema(kind Kind) tmpl.Schema {
	s := tmpl.Schema{}
	for _, f := range Fields(kind) {
		s[f.Path] = f
	}
	return s
}

// KindInfoOf returns the catalogue entry of a kind.
func KindInfoOf(kind Kind) (KindInfo, bool) {
	for _, k := range Catalogue() {
		if k.Kind == kind {
			return k, true
		}
	}
	return KindInfo{}, false
}

// ToValue converts a view (struct or pointer to struct of docs/views) to a template value tree.
func ToValue(v any) tmpl.Value { return toValue(reflect.ValueOf(v)) }

func toValue(v reflect.Value) tmpl.Value {
	if !v.IsValid() {
		return tmpl.Value{Null: true, Type: tmpl.TObject}
	}
	t := v.Type()
	ft := fieldType(t)
	if t.Kind() == reflect.Pointer {
		if v.IsNil() {
			return tmpl.Value{Type: ft, Null: true}
		}
		return toValue(v.Elem())
	}
	switch ft {
	case tmpl.TDate:
		d, _ := v.Interface().(calendar.Date)
		if d.IsZero() {
			return tmpl.Value{Type: tmpl.TDate}
		}
		return tmpl.Value{Type: tmpl.TDate, S: d.String()}
	case tmpl.TDateTime:
		t, _ := v.Interface().(time.Time)
		return tmpl.Value{Type: tmpl.TDateTime, T: t}
	case tmpl.TCents, tmpl.TCm, tmpl.TBP, tmpl.TMillis, tmpl.TInt:
		return tmpl.Value{Type: ft, N: v.Int()}
	case tmpl.TBool:
		return tmpl.Value{Type: tmpl.TBool, B: v.Bool()}
	case tmpl.TText, tmpl.TNotes:
		return tmpl.Value{Type: ft, S: v.String()}
	case tmpl.TList:
		out := tmpl.Value{Type: tmpl.TList, L: make([]tmpl.Value, 0, v.Len())}
		for i := range v.Len() {
			out.L = append(out.L, toValue(v.Index(i)))
		}
		return out
	}
	out := tmpl.Value{Type: tmpl.TObject, O: map[string]tmpl.Value{}}
	for i := range t.NumField() {
		name, _, ok := docTag(t.Field(i))
		if !ok {
			continue
		}
		out.O[name] = toValue(v.Field(i))
	}
	return out
}

// decodeView decodes a JSON snapshot into the view struct of kind.
func decodeView(kind Kind, raw []byte) (any, error) {
	info, ok := KindInfoOf(kind)
	if !ok || info.View == nil {
		return nil, fmt.Errorf("docs: kind %s has no view", kind)
	}
	p := reflect.New(info.View)
	if err := json.Unmarshal(raw, p.Interface()); err != nil {
		return nil, fmt.Errorf("docs: snapshot: %w", err)
	}
	return p.Interface(), nil
}
