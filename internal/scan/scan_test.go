package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "api"))
	mkRepo(t, filepath.Join(root, "org", "web"))
	mkRepo(t, filepath.Join(root, "org", "deep", "worker"))
	mkRepo(t, filepath.Join(root, "api", "vendor", "nested")) // inside a repo
	mkRepo(t, filepath.Join(root, ".hidden", "secret"))
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	found, err := Find(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	var rels []string
	for _, repo := range found {
		rels = append(rels, repo.Rel)
	}
	want := []string{"api", filepath.Join("org", "deep", "worker"), filepath.Join("org", "web")}
	if len(rels) != len(want) {
		t.Fatalf("found %v, want %v", rels, want)
	}
	for i := range want {
		if rels[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, rels[i], want[i])
		}
	}
	if found[0].Name != "api" || !filepath.IsAbs(found[0].Path) {
		t.Errorf("found[0] = %+v, want an absolute path named api", found[0])
	}
}

func TestFindRespectsDepth(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a", "b", "c", "deep"))

	found, err := Find(context.Background(), Options{Root: root, Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("found %v below the depth limit", found)
	}
}

func TestFindRootIsItselfARepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)

	found, err := Find(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Path != root {
		t.Fatalf("found %+v, want the root itself", found)
	}
}
