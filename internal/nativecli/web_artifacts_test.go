package nativecli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const artifactPreviewPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aEioAAAAASUVORK5CYII="

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
	scanVisualizations(context.Background(), root, id, func(path string) bool {
		_ = registerArtifact(context.Background(), state, root, "visualization", path, &files)
		return true
	})
	if len(files) != 1 || files[0].Name != "report.html" || files[0].MIME != "application/octet-stream" {
		t.Fatalf("unexpected generated files: %+v", files)
	}
	for _, path := range []string{"../escape", "/etc/passwd", "alias/report.txt", ".ssh/id", "results/../results/report.txt"} {
		if err := registerArtifact(context.Background(), state, workspace, "workspace", path, &files); err == nil {
			t.Errorf("registered out-of-scope file %q", path)
		}
	}
	register := func() {
		t.Helper()
		if err := registerArtifact(context.Background(), state, workspace, "workspace", "results/report.txt", &files); err != nil {
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

func TestWebArtifactsDiscoverNativeMessageFiles(t *testing.T) {
	ctx := context.Background()
	workspace, home, accounts, state := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	const thread = "00000000-0000-4000-8000-000000000061"
	const other = "00000000-0000-4000-8000-000000000062"
	png, err := base64.StdEncoding.DecodeString(artifactPreviewPNG)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	image := filepath.Join(workspace, "media", "chart (new).png")
	generated := filepath.Join(home, "generated_images", thread, "ig_saved.png")
	accountImage := filepath.Join(accounts, "fixture", "generated_images", thread, "ig_account.png")
	attachment := filepath.Join(home, "attachments", "stored.png")
	foreign := filepath.Join(home, "generated_images", other, "ig_other.png")
	document := filepath.Join(workspace, "media", "report.txt")
	for _, path := range []string{image, generated, accountImage, attachment, foreign} {
		write(path, png)
	}
	write(document, []byte("report"))
	write(filepath.Join(workspace, ".env"), []byte("private"))
	if err := os.Symlink(foreign, filepath.Join(workspace, "linked.png")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "workspace-parent")
	if err := os.Symlink(filepath.Dir(workspace), alias); err != nil {
		t.Fatal(err)
	}
	aliasedImage := filepath.Join(alias, filepath.Base(workspace), "media", filepath.Base(image))
	toItem := func(item map[string]any) json.RawMessage {
		t.Helper()
		b, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	body := fmt.Sprintf("![Chart][chart]\n\n[chart]: <./media/chart (new).png>\n\n`%s`\n\n[다른 대화](<%s>) [숨김](.env) [링크](linked.png)", document, foreign)
	turn := WebTurn{ID: "turn", Items: []json.RawMessage{
		toItem(map[string]any{"type": "userMessage", "id": "input", "content": []map[string]string{
			{"type": "image", "url": "data:image/png;base64," + artifactPreviewPNG},
			{"type": "localImage", "path": aliasedImage},
			{"type": "localImage", "path": attachment},
		}}),
		toItem(map[string]any{"type": "imageGeneration", "id": "output", "status": "completed", "savedPath": generated}),
		toItem(map[string]any{"type": "imageGeneration", "id": "account-output", "status": "completed", "savedPath": accountImage}),
		toItem(map[string]any{"type": "agentMessage", "id": "answer", "text": body}),
	}}
	batch := webArtifactBatch{dir: state, home: home, accounts: accounts, cwd: workspace, thread: thread}
	batch.projectTurn(ctx, &turn)
	if err := batch.save(); err != nil {
		t.Fatal(err)
	}
	if len(turn.Artifacts) != 6 || len(turn.FileRefs) != 2 || turn.FileRefs["./media/chart (new).png"] == "" || turn.FileRefs[document] == "" {
		t.Fatalf("missing file references or unexpected registered path: %+v", turn.FileRefs)
	}
	if bytes.Contains(turn.Items[0], []byte(artifactPreviewPNG)) || !bytes.Contains(turn.Items[0], []byte(`"artifactId"`)) ||
		!bytes.Contains(turn.Items[1], []byte(`"type":"fileOutput"`)) {
		t.Fatal("native attachments were not projected into IDs")
	}
	files, err := loadArtifacts(state)
	if err != nil || len(files) != len(turn.Artifacts) {
		t.Fatal("artifact registry was not saved", err)
	}
	for i, file := range files {
		if strings.Contains(file.Name, "other") || file.Name == "linked.png" || file.Name == ".env" {
			t.Fatal("out-of-scope artifact was registered", file.Name)
		}
		if !file.Deferred {
			continue
		}
		if _, err := os.Stat(filepath.Join(state, file.snapshot())); !os.IsNotExist(err) {
			t.Fatal("history read copied a file before the first view")
		}
		root := batch.root(file.Source, file.Path)
		if err := materializeArtifact(ctx, state, root, files, i); err != nil {
			t.Fatal(err)
		}
		if snapshot, err := os.ReadFile(filepath.Join(state, file.snapshot())); err != nil || len(snapshot) != int(file.Size) {
			t.Fatal("download snapshot does not match the registered size", err)
		}
	}
}
