package shard

import (
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

// Slot returns the 8-bit shard slot for a document ID using CRC32 of the
// sharding prefix. Periods in the first six positions are ignored for
// prefix detection. If there is no later period, the whole ID is used.
func Slot(documentID string) byte {
	prefix := shardingPrefix(documentID)
	sum := crc32.ChecksumIEEE([]byte(prefix))
	return byte(sum & 0xFF)
}

// SlotHex returns the two-character uppercase hex form of Slot.
func SlotHex(documentID string) string {
	return fmt.Sprintf("%02X", Slot(documentID))
}

// RawSlot returns the 8-bit shard slot for an arbitrary byte string using
// CRC32, with no document-ID prefix detection. Search result shard slots
// use this form: the shard input is the encoded search directory path
// (joined with "/", no leading/trailing slash), not a document ID, so the
// period-prefix sharding rule from CONVENTIONS.md does not apply. See
// SEARCHING.md "Search Sharding".
func RawSlot(input string) byte {
	return byte(crc32.ChecksumIEEE([]byte(input)) & 0xFF)
}

// RawSlotHex returns the two-character uppercase hex form of RawSlot.
func RawSlotHex(input string) string {
	return fmt.Sprintf("%02X", RawSlot(input))
}

func shardingPrefix(documentID string) string {
	runes := []rune(documentID)
	for i, r := range runes {
		if r != '.' {
			continue
		}
		if i < 6 {
			continue
		}
		return string(runes[:i])
	}
	return documentID
}

// Range describes an inclusive hex shard range such as "00-7F".
type Range struct {
	Start byte
	End   byte
	Raw   string
}

// ParseRange parses "AA-BB" or a single slot "7A".
func ParseRange(raw string) (Range, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Range{}, fmt.Errorf("empty shard range")
	}
	if !strings.Contains(raw, "-") {
		slot, err := parseSlot(raw)
		if err != nil {
			return Range{}, err
		}
		return Range{Start: slot, End: slot, Raw: strings.ToUpper(raw)}, nil
	}
	parts := strings.Split(raw, "-")
	if len(parts) != 2 {
		return Range{}, fmt.Errorf("invalid shard range %q", raw)
	}
	start, err := parseSlot(parts[0])
	if err != nil {
		return Range{}, err
	}
	end, err := parseSlot(parts[1])
	if err != nil {
		return Range{}, err
	}
	if start > end {
		return Range{}, fmt.Errorf("shard range start after end: %q", raw)
	}
	return Range{Start: start, End: end, Raw: strings.ToUpper(raw)}, nil
}

func parseSlot(raw string) (byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 2 {
		return 0, fmt.Errorf("invalid shard slot %q", raw)
	}
	v, err := strconv.ParseUint(raw, 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid shard slot %q: %w", raw, err)
	}
	return byte(v), nil
}

// Contains reports whether slot is inside r.
func (r Range) Contains(slot byte) bool {
	return slot >= r.Start && slot <= r.End
}

// ValidateFullCoverage ensures ranges cover every slot 0x00-0xFF without overlap.
func ValidateFullCoverage(ranges []Range) error {
	covered := make([]bool, 256)
	for _, r := range ranges {
		for i := int(r.Start); i <= int(r.End); i++ {
			if covered[i] {
				return fmt.Errorf("overlapping shard slot %02X", i)
			}
			covered[i] = true
		}
	}
	for i, ok := range covered {
		if !ok {
			return fmt.Errorf("incomplete shard map: missing slot %02X", i)
		}
	}
	return nil
}
