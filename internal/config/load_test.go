package config

import "testing"

func TestLoadSampleConfig(t *testing.T) {
	cfg, err := Load("../../testdata/sample-config")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.General.EstablishmentServer != "serverA" {
		t.Fatalf("unexpected establishment server")
	}
	if _, ok := cfg.Schemas["Movies"]; !ok {
		t.Fatalf("expected Movies schema")
	}
}
