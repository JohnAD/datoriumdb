// Package schemapatch applies the schema-upgrade update list described in
// UPDATE-SCHEMA.md to an OJSON collection schema document. It is
// intentionally limited to the structural schema transformation needed by
// `datoriumctl collection upgrade`; per-document migration is out of scope
// for the MVP CLI.
package schemapatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/JohnAD/ojson"
)

// omap is an insertion-order-preserving JSON object used while parsing the
// upgrade-request file, so new schema fields can be constructed with the
// same key order the operator wrote in the "schema" object.
type omap struct {
	keys []string
	vals map[string]any
}

func newOMap() *omap {
	return &omap{vals: map[string]any{}}
}

func (m *omap) Set(key string, value any) {
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = value
}

func (m *omap) Get(key string) (any, bool) {
	v, ok := m.vals[key]
	return v, ok
}

func (m *omap) Keys() []string {
	return m.keys
}

// decodeOrdered parses JSON bytes into a tree of *omap (objects), []any
// (arrays), and scalar types (string, json.Number, bool, nil), preserving
// object key order.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected trailing JSON content")
	}
	return value, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return decodeOrderedObject(dec)
		case '[':
			return decodeOrderedArray(dec)
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", t)
		}
	case string, json.Number, bool, nil:
		return t, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %v", t)
	}
}

func decodeOrderedObject(dec *json.Decoder) (*omap, error) {
	m := newOMap()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key must be a string")
		}
		val, err := decodeOrderedValue(dec)
		if err != nil {
			return nil, err
		}
		m.Set(key, val)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeOrderedArray(dec *json.Decoder) ([]any, error) {
	arr := []any{}
	for dec.More() {
		val, err := decodeOrderedValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return arr, nil
}

// toJSONValue converts an ordered-decode tree into an ojson.JSONValue,
// preserving object key order via sequential Set calls.
func toJSONValue(v any) ojson.JSONValue {
	switch t := v.(type) {
	case *omap:
		obj := ojson.NewObject()
		for _, k := range t.keys {
			val, _ := t.Get(k)
			obj.Set(k, toJSONValue(val))
		}
		return obj
	case []any:
		arr := ojson.NewArray()
		for _, item := range t {
			arr.Append(toJSONValue(item))
		}
		return arr
	case string:
		return ojson.NewString(t)
	case json.Number:
		val, err := ojson.NewNumberTry(t.String())
		if err != nil {
			return ojson.NewVoid()
		}
		return val
	case bool:
		return ojson.NewBoolean(t)
	case nil:
		return ojson.NewNull()
	default:
		return ojson.NewVoid()
	}
}

// toPlainValue converts an ordered-decode tree (as produced by
// decodeOrdered) into plain Go values matching how encoding/json decodes
// into map[string]any/[]any/float64/etc., so schema-upgrade "value" and
// "failover" literals can be compared with and written into document
// content decoded the normal way.
func toPlainValue(v any) any {
	switch t := v.(type) {
	case *omap:
		out := map[string]any{}
		for _, k := range t.keys {
			val, _ := t.Get(k)
			out[k] = toPlainValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = toPlainValue(item)
		}
		return out
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0.0
		}
		return f
	default:
		return t
	}
}

func getString(m *omap, key string) (string, bool) {
	v, ok := m.Get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getInt(m *omap, key string) (int, bool, error) {
	v, ok := m.Get(key)
	if !ok {
		return 0, false, nil
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, true, fmt.Errorf("%q must be a number", key)
	}
	i, err := n.Int64()
	if err != nil {
		return 0, true, fmt.Errorf("%q must be an integer: %w", key, err)
	}
	return int(i), true, nil
}

func getArray(m *omap, key string) ([]any, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	arr, ok := v.([]any)
	return arr, ok
}

func getObject(m *omap, key string) (*omap, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	obj, ok := v.(*omap)
	return obj, ok
}
