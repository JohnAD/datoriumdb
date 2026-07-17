package search

import "testing"

func TestResolveQueryPathMatchesEvaluateDocument(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[
			{"field":"/status","op":"equals","value":"released"},
			{"field":"/genre","op":"in","value":["scifi","drama"],"select":"$genre"},
			{"field":"/highRated","op":"exists","value":"$hasHighRated"}
		],"sort":[]}}`)

	doc := map[string]any{"status": "released", "genre": "scifi", "highRated": true}
	docRes, err := EvaluateDocument(def, doc)
	if err != nil || !docRes.Matched {
		t.Fatalf("EvaluateDocument failed: %+v err=%v", docRes, err)
	}

	segments, err := ResolveQueryPath(def, map[string]any{"genre": "scifi", "hasHighRated": true})
	if err != nil {
		t.Fatalf("ResolveQueryPath: %v", err)
	}
	if len(segments) != len(docRes.Segments) {
		t.Fatalf("segment count mismatch: query=%v doc=%v", segments, docRes.Segments)
	}
	for i := range segments {
		if segments[i] != docRes.Segments[i] {
			t.Fatalf("segment %d mismatch: query=%q doc=%q", i, segments[i], docRes.Segments[i])
		}
	}
}

func TestResolveQueryPathInRequiresAllowedValue(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"in","value":["scifi","drama"],"select":"$genre"}],"sort":[]}}`)

	if _, err := ResolveQueryPath(def, map[string]any{"genre": "horror"}); err == nil {
		t.Fatalf("expected an error for a genre outside the allowed constants")
	}
	if _, err := ResolveQueryPath(def, map[string]any{}); err == nil {
		t.Fatalf("expected an error for a missing required select variable")
	}
}

func TestResolveQueryPathExistsRequiresBoolean(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre"}],"sort":[]}}`)
	if _, err := ResolveQueryPath(def, map[string]any{"hasGenre": "yes"}); err == nil {
		t.Fatalf("expected an error for a non-boolean truth variable")
	}
	segs, err := ResolveQueryPath(def, map[string]any{"hasGenre": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segs) != 1 || segs[0] != "false" {
		t.Fatalf("unexpected segments: %v", segs)
	}
}

func TestResolveQueryPathEqualsConstantNoSegment(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],"sort":[]}}`)
	segs, err := ResolveQueryPath(def, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segs) != 0 {
		t.Fatalf("expected zero segments for a pure constant filter clause, got %v", segs)
	}
}
