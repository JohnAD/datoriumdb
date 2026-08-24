// Package commandreq defines the public JSON command request contract:
// {command, target, parameter, detail} as described in docs/api.md.
package commandreq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Request is one public command. Detail is always a JSON object (possibly {}).
type Request struct {
	Command   string          `json:"command"`
	Target    string          `json:"target"`
	Parameter string          `json:"parameter"`
	Detail    json.RawMessage `json:"detail"`
}

// DecodeJSON parses and validates a four-field command request from a JSON body.
// Unknown root fields are rejected. detail must be a JSON object.
func DecodeJSON(data []byte) (Request, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Request{}, fmt.Errorf("invalid JSON command body: %w", err)
	}
	allowed := map[string]bool{
		"command": true, "target": true, "parameter": true, "detail": true,
	}
	for k := range raw {
		if !allowed[k] {
			return Request{}, fmt.Errorf("unknown root field %q", k)
		}
	}
	var req Request
	if v, ok := raw["command"]; ok {
		if err := json.Unmarshal(v, &req.Command); err != nil {
			return Request{}, fmt.Errorf("command must be a string")
		}
	}
	if v, ok := raw["target"]; ok {
		if err := json.Unmarshal(v, &req.Target); err != nil {
			return Request{}, fmt.Errorf("target must be a string")
		}
	}
	if v, ok := raw["parameter"]; ok {
		if err := json.Unmarshal(v, &req.Parameter); err != nil {
			return Request{}, fmt.Errorf("parameter must be a string")
		}
	}
	detail, ok := raw["detail"]
	if !ok {
		return Request{}, fmt.Errorf("detail is required")
	}
	detail = json.RawMessage(bytes.TrimSpace(detail))
	if len(detail) == 0 || detail[0] != '{' {
		return Request{}, fmt.Errorf("detail must be a JSON object")
	}
	// Reject non-objects (arrays/null) via a throwaway decode.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(detail, &obj); err != nil {
		return Request{}, fmt.Errorf("detail must be a JSON object: %w", err)
	}
	req.Detail = detail
	req.Command = strings.TrimSpace(req.Command)
	req.Target = strings.TrimSpace(req.Target)
	req.Parameter = strings.TrimSpace(req.Parameter)
	if req.Command == "" {
		return Request{}, fmt.Errorf("command is required")
	}
	if req.Target == "" {
		return Request{}, fmt.Errorf("target is required")
	}
	// parameter may be empty for commands that do not use it (none today),
	// but the contract still expects the field; empty string is allowed for
	// fileList where the document id is the parameter.
	return req, nil
}

// New builds a Request from typed fields. detail must marshal to a JSON object.
func New(command, target, parameter string, detail any) (Request, error) {
	raw, err := json.Marshal(detail)
	if err != nil {
		return Request{}, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return Request{}, fmt.Errorf("detail must be a JSON object")
	}
	return Request{
		Command:   strings.TrimSpace(command),
		Target:    strings.TrimSpace(target),
		Parameter: strings.TrimSpace(parameter),
		Detail:    raw,
	}, nil
}

// Must is New that panics on error; intended for tests and static fixtures.
func Must(command, target, parameter string, detail any) Request {
	req, err := New(command, target, parameter, detail)
	if err != nil {
		panic(err)
	}
	return req
}

// DetailMap unmarshals detail into a generic map using json.Number.
func (r Request) DetailMap() (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(r.Detail))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid detail object: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// StringField returns a string detail field, accepting JSON numbers as text.
func StringField(detail map[string]any, key string) string {
	v, ok := detail[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}
