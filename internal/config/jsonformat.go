package config

import (
	"github.com/JohnAD/ojson"
)

// PrettyJSONBytes parses raw with OJSON (preserving field order) and
// returns 2-space-indented JSON with a trailing newline. Compact or
// oddly spaced input is normalized for git-friendly storage without
// reordering object fields.
func PrettyJSONBytes(raw []byte) ([]byte, error) {
	value, err := ojson.ReadBytesNoSchema(raw)
	if err != nil {
		return nil, err
	}
	out := value.ToPrettyJSONBytes(2)
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}
