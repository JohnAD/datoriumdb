package envelope

import "encoding/json"

// Error is one application-level error entry.
type Error struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

// Result is the common DatoriumDB API envelope.
type Result map[string]any

// OK builds a success envelope with ok:true and extra fields.
func OK(fields map[string]any) Result {
	out := Result{"ok": true}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// Fail builds a failure envelope with ok:false and errors.
func Fail(fields map[string]any, errs ...Error) Result {
	out := Result{"ok": false}
	for k, v := range fields {
		out[k] = v
	}
	list := make([]Error, 0, len(errs))
	list = append(list, errs...)
	out["errors"] = list
	return out
}

// Bytes marshals the envelope as compact JSON.
func (r Result) Bytes() ([]byte, error) {
	return json.Marshal(r)
}
