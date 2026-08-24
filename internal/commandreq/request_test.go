package commandreq

import "testing"

func TestDecodeJSONHappy(t *testing.T) {
	req, err := DecodeJSON([]byte(`{"command":"create","target":"Movies","parameter":"id1","detail":{"title":"X"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Command != "create" || req.Target != "Movies" || req.Parameter != "id1" {
		t.Fatalf("%#v", req)
	}
	m, err := req.DetailMap()
	if err != nil || m["title"] != "X" {
		t.Fatalf("%#v %v", m, err)
	}
}

func TestDecodeJSONRejectsUnknownRoot(t *testing.T) {
	_, err := DecodeJSON([]byte(`{"command":"read","target":"Movies","parameter":"id1","detail":{},"extra":1}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeJSONRequiresObjectDetail(t *testing.T) {
	_, err := DecodeJSON([]byte(`{"command":"read","target":"Movies","parameter":"id1","detail":[]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
