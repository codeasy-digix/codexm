package nativecli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebArtifactsRegisterScopedSnapshots(t *testing.T) {
	workspace, state := t.TempDir(), t.TempDir()
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const id = "00000000-0000-4000-8000-000000000061"
	const other = "00000000-0000-4000-8000-000000000062"
	root := filepath.Join(workspace, "visualizations")
	write(filepath.Join(root, "2026", "01", "02", id, "report.html"), "<script>fixture</script>")
	write(filepath.Join(root, "2026", "01", "02", other, "private.txt"), "other chat")
	write(filepath.Join(workspace, "results", "report.txt"), "first revision")
	if err := os.Symlink("results", filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	files, err := loadArtifacts(state)
	if err != nil {
		t.Fatal(err)
	}
	scanVisualizations(context.Background(), root, id, state, &files)
	if len(files) != 1 || files[0].Name != "report.html" || files[0].MIME != "application/octet-stream" {
		t.Fatalf("unexpected generated files: %+v", files)
	}
	for _, path := range []string{"../escape", "/etc/passwd", "alias/report.txt", ".ssh/id", "results/../results/report.txt"} {
		if err := registerArtifact(state, workspace, "workspace", path, &files); err == nil {
			t.Errorf("registered out-of-scope file %q", path)
		}
	}
	register := func() {
		t.Helper()
		if err := registerArtifact(state, workspace, "workspace", "results/report.txt", &files); err != nil {
			t.Fatal(err)
		}
	}
	register()
	first := files[1]
	write(filepath.Join(workspace, "results", "report.txt"), "a newer and longer version")
	snapshot, err := os.ReadFile(filepath.Join(state, first.snapshot()))
	if err != nil || string(snapshot) != "first revision" {
		t.Fatalf("registered copy changed: %q, %v", snapshot, err)
	}
	register()
	if len(files) != 2 || files[1].ID != first.ID || files[1].Version == first.Version {
		t.Fatalf("re-registration did not replace the earlier snapshot: %+v", files)
	}
	if _, err := os.Stat(filepath.Join(state, first.snapshot())); !os.IsNotExist(err) {
		t.Fatal("earlier snapshot should have been removed")
	}
	if saved, err := loadArtifacts(state); err != nil || len(saved) != 2 || saved[1] != files[1] {
		t.Fatalf("registry did not persist: %+v, %v", saved, err)
	}
}
