package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func parseSingleByteRange(raw string, size int64) (start, length int64, partial bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, size, false, nil
	}
	// RFC 7233: unrecognized range units are ignored; return the full body.
	if !strings.HasPrefix(raw, "bytes=") {
		return 0, size, false, nil
	}
	if size <= 0 {
		return 0, 0, false, fmt.Errorf("invalid byte range")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(raw, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf("exactly one byte range is supported")
	}
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false, fmt.Errorf("invalid byte range")
	}
	if left == "" {
		suffix, parseErr := parseRangeNumber(right)
		if parseErr != nil || suffix == 0 {
			return 0, 0, false, fmt.Errorf("invalid suffix byte range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, true, nil
	}
	start, parseErr := parseRangeNumber(left)
	if parseErr != nil || start >= size {
		return 0, 0, false, fmt.Errorf("byte range start is unsatisfiable")
	}
	end := size - 1
	if right != "" {
		end, parseErr = parseRangeNumber(right)
		if parseErr != nil || end < start {
			return 0, 0, false, fmt.Errorf("invalid byte range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, true, nil
}

func parseRangeNumber(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("range number is empty")
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("range number must contain only digits")
		}
	}
	return strconv.ParseInt(raw, 10, 64)
}

func writeJSONStatus(w http.ResponseWriter, status int, result envelope.Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(result)
}
