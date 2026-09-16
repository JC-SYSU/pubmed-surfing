package runtimectl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveSafetyAndPayload(t *testing.T) {
	for _, name := range []string{"../x", "a/../../x", "/tmp/x", `a\b`} {
		if safeName(name) {
			t.Fatalf("unsafe name accepted: %q", name)
		}
	}
	if !safeName("bin/pubmed-surfing") {
		t.Fatal("safe name rejected")
	}
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "pubmed-surfing"), []byte("binary"), 0755); e != nil {
		t.Fatal(e)
	}
	sum, e := digest(filepath.Join(dir, "pubmed-surfing"))
	if e != nil {
		t.Fatal(e)
	}
	m := Manifest{Payload: map[string]string{"pubmed-surfing": sum}}
	if e = verifyPayload(dir, m); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "pubmed-surfing"), []byte("changed"), 0755); e != nil {
		t.Fatal(e)
	}
	if e = verifyPayload(dir, m); e == nil {
		t.Fatal("checksum mismatch accepted")
	}
}

func TestDefaultHomeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PUBMED_SURFING_GO_HOME", home)
	if DefaultHome() != home {
		t.Fatal(DefaultHome())
	}
}

func TestVerifyResolvesRuntimeSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "current")
	if e := os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	resolved, e := resolveRuntimePath(link)
	if e != nil {
		t.Fatal(e)
	}
	want, e := filepath.EvalSymlinks(target)
	if e != nil {
		t.Fatal(e)
	}
	if resolved != want {
		t.Fatalf("resolved %q, want %q", resolved, want)
	}
}
