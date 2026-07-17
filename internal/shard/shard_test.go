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
