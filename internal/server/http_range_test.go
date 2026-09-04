package server

import "testing"

func TestParseSingleByteRange(t *testing.T) {
	tests := []struct {
		raw            string
		start, length  int64
		partial, fails bool
	}{
		{"", 0, 10, false, false},
		{"bytes=2-5", 2, 4, true, false},
		{"bytes=6-", 6, 4, true, false},
		{"bytes=-3", 7, 3, true, false},
		{"bytes=-20", 0, 10, true, false},
		{"bytes=2-99", 2, 8, true, false},
		{"bytes=10-", 0, 0, false, true},
		{"bytes=5-2", 0, 0, false, true},
		{"bytes=0-1,4-5", 0, 0, false, true},
		{"items=0-1", 0, 10, false, false}, // unknown unit ignored → full body
		{"not-a-range", 0, 10, false, false},
		{"bytes=+1-2", 0, 0, false, true},
		{"bytes=1-+2", 0, 0, false, true},
		{"bytes=-0", 0, 0, false, true},
		{"bytes=9223372036854775808-", 0, 0, false, true},
	}
	for _, tc := range tests {
		start, length, partial, err := parseSingleByteRange(tc.raw, 10)
		if (err != nil) != tc.fails || start != tc.start || length != tc.length || partial != tc.partial {
			t.Errorf("%q: got (%d,%d,%v,%v), want (%d,%d,%v,fails=%v)",
				tc.raw, start, length, partial, err, tc.start, tc.length, tc.partial, tc.fails)
		}
	}
}

func TestParseSingleByteRangeEmptyFile(t *testing.T) {
	start, length, partial, err := parseSingleByteRange("", 0)
	if err != nil || start != 0 || length != 0 || partial {
		t.Fatalf("full empty read: got (%d,%d,%v,%v)", start, length, partial, err)
	}
	if _, _, _, err := parseSingleByteRange("bytes=0-", 0); err == nil {
		t.Fatal("range on empty file must be unsatisfiable")
	}
}
