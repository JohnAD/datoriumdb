//go:build contract

package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func goldenPath(name string) string {
	return filepath.Join("golden", name+".json")
}

// AssertGolden normalizes got and compares it against the checked-in
// golden/{name}.json fixture. Set DATORIUMDB_UPDATE_GOLDEN=1 to write/update
// the fixture instead of failing (used only when intentionally changing
// envelope shapes).
func AssertGolden(t *testing.T, name string, got any) {
	t.Helper()
	normalized := NormalizedJSON(got)
	path := goldenPath(name)
	if updateGolden() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with DATORIUMDB_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != normalized {
		t.Fatalf("envelope for %q does not match golden %s\n--- want ---\n%s\n--- got ---\n%s", name, path, want, normalized)
	}
}
