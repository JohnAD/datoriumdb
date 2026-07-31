package shard

import "testing"

func TestSlotDeterministicAndHex(t *testing.T) {
	id := "01KWD65CFQPEZS7H1WJE4MK990"
	a := Slot(id)
	b := Slot(id)
	if a != b {
		t.Fatalf("slot not deterministic: %02X vs %02X", a, b)
	}
	if SlotHex(id) != formatByte(a) {
		t.Fatalf("hex mismatch")
	}
}

func TestShardingPrefixUsesFirstPeriodAfterSix(t *testing.T) {
	base := "01KWD65CFQPEZS7H1WJE4MK990"
	related := base + ".settings"
	if Slot(base) != Slot(related) {
		t.Fatalf("related ID should share shard with prefix")
	}
}

func TestShardingPrefixIgnoresPeriodsInFirstSixPositions(t *testing.T) {
	// Period at rune index 5 (0-based) is ignored: whole ID is hashed.
	early := "01234.rest"
	if Slot(early) == Slot("01234") {
		t.Fatalf("period before position 6 must not select a prefix; Slot(%q)=%02X Slot(%q)=%02X",
			early, Slot(early), "01234", Slot("01234"))
	}
	// Period at rune index 6 splits: prefix is the first six runes.
	qualifying := "012345.todo"
	if Slot(qualifying) != Slot("012345") {
		t.Fatalf("period at position 6+ must hash only the prefix: Slot(%q)=%02X Slot(%q)=%02X",
			qualifying, Slot(qualifying), "012345", Slot("012345"))
	}
	if Slot(qualifying) != Slot("012345.other") {
		t.Fatalf("related suffixes after the same qualifying prefix must share a slot")
	}
}

func TestParseRangeAndCoverage(t *testing.T) {
	r1, err := ParseRange("00-7F")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ParseRange("80-FF")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFullCoverage([]Range{r1, r2}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFullCoverage([]Range{r1}); err == nil {
		t.Fatal("expected incomplete coverage error")
	}
}

func formatByte(b byte) string {
	const hexdigits = "0123456789ABCDEF"
	return string([]byte{hexdigits[b>>4], hexdigits[b&0x0F]})
}
