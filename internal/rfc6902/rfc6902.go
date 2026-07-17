// Package rfc6902 applies a small, dependency-free subset of RFC 6902 JSON
// Patch operations to decoded document maps. It is shared by
// internal/engine (user-submitted patches) and internal/replication
// (SOT-authored replication patches applied by read/proxy members), so both
// see identical patch semantics.
package rfc6902

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Apply applies one RFC 6902 operation (as decoded JSON: op/path/value/from)
// to doc in place.
func Apply(doc map[string]any, op map[string]any) error {
	operation, _ := op["op"].(string)
	path, _ := op["path"].(string)
	if path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /")
	}
	switch operation {
	case "add":
		return pointerAdd(doc, path, op["value"])
	case "replace":
		if _, err := pointerGet(doc, path); err != nil {
			return err
		}
		return pointerSet(doc, path, op["value"], false)
	case "remove":
		return pointerRemove(doc, path)
	case "test":
		actual, err := pointerGet(doc, path)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, op["value"]) {
			return fmt.Errorf("test failed at %s", path)
		}
		return nil
	case "move":
		from, _ := op["from"].(string)
		if from == "" {
			return fmt.Errorf("move requires from")
		}
		val, err := pointerGet(doc, from)
		if err != nil {
			return err
		}
		cloned := cloneJSON(val)
		if err := pointerRemove(doc, from); err != nil {
			return err
		}
		return pointerAdd(doc, path, cloned)
	case "copy":
		from, _ := op["from"].(string)
		if from == "" {
			return fmt.Errorf("copy requires from")
		}
		val, err := pointerGet(doc, from)
		if err != nil {
			return err
		}
		return pointerAdd(doc, path, cloneJSON(val))
	default:
		return fmt.Errorf("unsupported patch op %q", operation)
	}
}

func pointerGet(doc map[string]any, path string) (any, error) {
	parts := splitJSONPointer(path)
	if len(parts) == 0 {
		return doc, nil
	}
	cur := any(doc)
	for _, part := range parts {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, fmt.Errorf("missing path %s", path)
			}
			cur = next
		case []any:
			idx, ok := parseIndex(part, len(node), false)
			if !ok {
				return nil, fmt.Errorf("missing path %s", path)
			}
			cur = node[idx]
		default:
			return nil, fmt.Errorf("missing path %s", path)
		}
	}
	return cur, nil
}

func pointerAdd(doc map[string]any, path string, value any) error {
	return pointerSet(doc, path, value, true)
}

func pointerSet(doc map[string]any, path string, value any, allowAdd bool) error {
	parts := splitJSONPointer(path)
	if len(parts) == 0 {
		return fmt.Errorf("cannot replace document root")
	}
	return mutateParent(doc, parts[:len(parts)-1], func(parent any) error {
		key := parts[len(parts)-1]
		switch p := parent.(type) {
		case map[string]any:
			if !allowAdd {
				if _, ok := p[key]; !ok {
					return fmt.Errorf("missing path %s", path)
				}
			}
			p[key] = value
			return nil
		case *container:
			arr := p.slice
			if key == "-" {
				if !allowAdd {
					return fmt.Errorf("missing path %s", path)
				}
				p.set(append(arr, value))
				return nil
			}
			idx, err := strconv.Atoi(key)
			if err != nil {
				return fmt.Errorf("invalid array index %q", key)
			}
			if allowAdd {
				if idx < 0 || idx > len(arr) {
					return fmt.Errorf("array index out of bounds")
				}
				if idx == len(arr) {
					p.set(append(arr, value))
					return nil
				}
				out := make([]any, 0, len(arr)+1)
				out = append(out, arr[:idx]...)
				out = append(out, value)
				out = append(out, arr[idx:]...)
				p.set(out)
				return nil
			}
			if idx < 0 || idx >= len(arr) {
				return fmt.Errorf("array index out of bounds")
			}
			arr[idx] = value
			p.set(arr)
			return nil
		default:
			return fmt.Errorf("path parent is not object or array")
		}
	})
}

func pointerRemove(doc map[string]any, path string) error {
	parts := splitJSONPointer(path)
	if len(parts) == 0 {
		return fmt.Errorf("cannot remove document root")
	}
	return mutateParent(doc, parts[:len(parts)-1], func(parent any) error {
		key := parts[len(parts)-1]
		switch p := parent.(type) {
		case map[string]any:
			if _, ok := p[key]; !ok {
				return fmt.Errorf("missing path %s", path)
			}
			delete(p, key)
			return nil
		case *container:
			idx, ok := parseIndex(key, len(p.slice), false)
			if !ok {
				return fmt.Errorf("missing path %s", path)
			}
			out := append([]any{}, p.slice[:idx]...)
			out = append(out, p.slice[idx+1:]...)
			p.set(out)
			return nil
		default:
			return fmt.Errorf("path parent is not object or array")
		}
	})
}

// container lets nested array mutations write the new slice header back to its owner.
type container struct {
	slice []any
	set   func([]any)
}

func mutateParent(doc map[string]any, parentParts []string, fn func(parent any) error) error {
	if len(parentParts) == 0 {
		return fn(doc)
	}
	var walk func(cur any, parts []string) error
	walk = func(cur any, parts []string) error {
		if len(parts) == 0 {
			return fn(cur)
		}
		part := parts[0]
		rest := parts[1:]
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return fmt.Errorf("missing parent path")
			}
			if arr, ok := next.([]any); ok && len(rest) == 0 {
				return fn(&container{
					slice: arr,
					set:   func(v []any) { node[part] = v },
				})
			}
			if arr, ok := next.([]any); ok {
				return walk(&container{
					slice: arr,
					set:   func(v []any) { node[part] = v },
				}, rest)
			}
			return walk(next, rest)
		case *container:
			idx, ok := parseIndex(part, len(node.slice), false)
			if !ok {
				return fmt.Errorf("missing parent path")
			}
			next := node.slice[idx]
			if arr, ok := next.([]any); ok && len(rest) == 0 {
				return fn(&container{
					slice: arr,
					set: func(v []any) {
						node.slice[idx] = v
						node.set(node.slice)
					},
				})
			}
			if arr, ok := next.([]any); ok {
				return walk(&container{
					slice: arr,
					set: func(v []any) {
						node.slice[idx] = v
						node.set(node.slice)
					},
				}, rest)
			}
			return walk(next, rest)
		default:
			return fmt.Errorf("missing parent path")
		}
	}
	return walk(doc, parentParts)
}

func parseIndex(part string, length int, allowEnd bool) (int, bool) {
	if part == "-" {
		if allowEnd {
			return length, true
		}
		return 0, false
	}
	idx, err := strconv.Atoi(part)
	if err != nil || idx < 0 || idx >= length {
		return 0, false
	}
	return idx, true
}

func splitJSONPointer(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	raw := strings.Split(strings.TrimPrefix(path, "/"), "/")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		out = append(out, p)
	}
	return out
}

func cloneJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = cloneJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = cloneJSON(val)
		}
		return out
	default:
		return v
	}
}
