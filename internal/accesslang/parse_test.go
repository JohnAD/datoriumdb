package accesslang

import "testing"

func TestParseCommandAndDetail(t *testing.T) {
	cmd, err := Parse(`create Movies null {$: Movies:0, title: "The Matrix", releaseYear: 1999}`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Word != "create" || cmd.Target != "Movies" || cmd.Parm != "null" {
		t.Fatalf("unexpected command parts: %+v", cmd)
	}
	detail, err := ParseDetail(cmd.Detail)
	if err != nil {
		t.Fatal(err)
	}
	if detail["$"] != "Movies:0" {
		t.Fatalf("expected Movies:0, got %#v", detail["$"])
	}
	if detail["title"] != "The Matrix" {
		t.Fatalf("expected title, got %#v", detail["title"])
	}
}

func TestParseRejectsMissingDetail(t *testing.T) {
	if _, err := Parse("read Movies 01ABC"); err == nil {
		t.Fatal("expected error")
	}
}
