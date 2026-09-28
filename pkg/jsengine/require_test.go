package jsengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequire_RelativeModuleFromBaseDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helpers.js"),
		[]byte(`module.exports = { double: function(n){ return n * 2; }, name: "helpers" };`), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	defer e.Close()
	e.SetRequireBaseDir(dir)

	v, err := e.Eval(`require("./helpers.js").double(21)`)
	if err != nil {
		t.Fatalf("require eval failed: %v", err)
	}
	if n, _ := v.(int64); n != 42 {
		t.Errorf("double(21) = %v, want 42", v)
	}

	v2, err := e.Eval(`require("./helpers.js").name`)
	if err != nil {
		t.Fatalf("require eval failed: %v", err)
	}
	if v2 != "helpers" {
		t.Errorf("name = %v, want helpers", v2)
	}
}

func TestRequire_NestedRequire(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.js"), []byte(`module.exports = { v: require("./b.js").v + 1 };`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.js"), []byte(`module.exports = { v: 10 };`), 0o644)

	e := New()
	defer e.Close()
	e.SetRequireBaseDir(dir)

	v, err := e.Eval(`require("./a.js").v`)
	if err != nil {
		t.Fatalf("nested require failed: %v", err)
	}
	if n, _ := v.(int64); n != 11 {
		t.Errorf("a.v = %v, want 11", v)
	}
}

func TestRequire_MissingModuleIsAnError(t *testing.T) {
	e := New()
	defer e.Close()
	e.SetRequireBaseDir(t.TempDir())

	if _, err := e.Eval(`require("./does-not-exist.js")`); err == nil {
		t.Error("expected an error requiring a missing module")
	}
}
