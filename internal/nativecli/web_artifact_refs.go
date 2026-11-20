package nativecli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const inlineArtifactLimit = 16 << 20

var (
	artifactScheme = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
	artifactLine   = regexp.MustCompile(`(?:#L[0-9]+(?:-L?[0-9]+)?|:[0-9]+(?::[0-9]+)?)$`)
	artifactMarker = regexp.MustCompile(`visualize([^\r\n]*?)`)
	artifactDate   = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[0-9]{2}/`)
	// Bare paths need a clear path prefix. Markdown links and code spans can
	// also supply filenames or paths containing spaces.
	artifactTextPath = regexp.MustCompile(`(?:^|[\s<(])((?:(?:file://(?:localhost)?|sandbox:)?/|(?:~|\.)/)[^\s<>'"\x60\])]+\.[A-Za-z0-9]{1,16}(?::[0-9]+(?::[0-9]+)?|#L[0-9]+(?:-L?[0-9]+)?)?)(?:$|[\s.,;>)\]])`)
)

func (b *webArtifactBatch) root(source, path string) string {
	if !artifactPath(path) {
		return ""
	}
	switch source {
	case "workspace":
		if cwd, err := filepath.EvalSymlinks(b.cwd); err == nil && filepath.IsAbs(cwd) && cwd != string(filepath.Separator) {
			_, insideNativeHome := relativeArtifactPath(b.home, cwd)
			_, insideAccountHomes := relativeArtifactPath(b.accounts, cwd)
			if !insideNativeHome && !insideAccountHomes {
				return cwd
			}
		}
	case "generated":
		if strings.HasPrefix(path, b.thread+"/") {
			return filepath.Join(b.home, "generated_images")
		}
	case "account-generated":
		parts := strings.Split(path, "/")
		if filepath.IsAbs(b.accounts) && len(parts) > 3 && config.Nickname.MatchString(parts[0]) && parts[1] == "generated_images" && parts[2] == b.thread {
			return b.accounts
		}
	case "visualization":
		parts := strings.Split(path, "/")
		if len(parts) > 4 && parts[3] == b.thread && artifactDate.MatchString(path) {
			return filepath.Join(b.home, "visualizations")
		}
	case "attachment":
		return filepath.Join(b.home, "attachments")
	}
	return ""
}

type webArtifactBatch struct {
	dir, home, accounts, cwd, thread string
	files                            []storedArtifact
	byID                             map[string]storedArtifact
	loaded, changed                  bool
	err                              error
	// Only embedded bytes are copied while preparing a transcript.
	embedded []string
}

func (b *webArtifactBatch) load() bool {
	if !b.loaded {
		b.loaded = true
		b.files, b.err = loadArtifacts(b.dir)
		b.byID = make(map[string]storedArtifact, len(b.files))
		for _, item := range b.files {
			b.byID[item.ID] = item
		}
	}
	return b.err == nil
}

func (b *webArtifactBatch) add(file storedArtifact) storedArtifact {
	b.files = append(b.files, file)
	b.byID[file.ID], b.changed = file, true
	return file
}

func (b *webArtifactBatch) save() error {
	if !b.changed {
		return b.err
	}
	if err := saveArtifacts(b.dir, b.files); err != nil {
		for _, snapshot := range b.embedded {
			_ = os.Remove(filepath.Join(b.dir, snapshot))
		}
		return err
	}
	return nil
}

func (b *webArtifactBatch) capturePath(source, path string) storedArtifact {
	root := b.root(source, path)
	if root == "" || !b.load() {
		return storedArtifact{}
	}
	key := artifactKey(source, path)
	if saved, ok := b.byID[key]; ok {
		return saved
	}
	file, info, err := openArtifactSource(root, path)
	if err != nil {
		return storedArtifact{}
	}
	defer file.Close()
	entry, err := describeArtifact(file, info, source, path)
	if err != nil || !artifactFits(b.files, entry) {
		return storedArtifact{}
	}
	entry.Deferred = true
	return b.add(entry)
}

func localArtifactPath(ref string) string {
	ref = strings.TrimSpace(ref)
	if len(ref) == 0 || len(ref) > 4096 || strings.HasPrefix(ref, "//") {
		return ""
	}
	lower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(lower, "file://"):
		uri, err := url.Parse(ref)
		if err != nil || uri.User != nil || (uri.Host != "" && !strings.EqualFold(uri.Host, "localhost")) || !filepath.IsAbs(uri.Path) {
			return ""
		}
		return artifactLine.ReplaceAllString(uri.Path, "")
	case strings.HasPrefix(lower, "sandbox:/") && !strings.HasPrefix(lower, "sandbox://"):
		ref = ref[len("sandbox:"):]
	case artifactScheme.MatchString(ref):
		return ""
	}
	if decoded, err := url.PathUnescape(ref); err == nil {
		ref = decoded
	}
	if strings.HasPrefix(ref, "~/") {
		ref = config.Expand(ref)
	}
	return artifactLine.ReplaceAllString(ref, "")
}

func localArtifactRelative(root, path string) (string, bool) {
	if !filepath.IsAbs(root) {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	return rel, err == nil && filepath.IsLocal(rel)
}

// macOS records temporary attachments under /var while the thread CWD can
// be canonicalized to /private/var. Find the exact root alias and still open
// every component below that root without following symlinks.
func relativeArtifactPath(root, path string) (string, bool) {
	if rel, ok := localArtifactRelative(root, path); ok {
		return rel, true
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(physical) || !filepath.IsAbs(path) {
		return "", false
	}
	if rel, ok := localArtifactRelative(physical, path); ok {
		return rel, true
	}
	for parent := path; ; parent = filepath.Dir(parent) {
		if filepath.Base(parent) == filepath.Base(physical) {
			if resolved, err := filepath.EvalSymlinks(parent); err == nil && resolved == physical {
				return localArtifactRelative(parent, path)
			}
		}
		if filepath.Dir(parent) == parent {
			return "", false
		}
	}
}

func (b *webArtifactBatch) captureReference(ref string) storedArtifact {
	path := localArtifactPath(ref)
	if path == "" {
		return storedArtifact{}
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return storedArtifact{}
		}
	}
	if !filepath.IsAbs(path) {
		if !artifactPath(path) {
			return storedArtifact{}
		}
		return b.capturePath("workspace", filepath.Clean(path))
	}
	// Keep native output directories scoped to their thread even when the
	// chat's working directory is an ancestor of the Codex home.
	for _, source := range []struct{ kind, dir string }{{"generated", "generated_images"}, {"visualization", "visualizations"}} {
		root := filepath.Join(b.home, source.dir)
		if rel, ok := relativeArtifactPath(root, path); ok {
			return b.capturePath(source.kind, rel)
		}
	}
	if _, inside := relativeArtifactPath(b.home, path); inside {
		return storedArtifact{}
	}
	if rel, inside := relativeArtifactPath(b.accounts, path); inside {
		return b.capturePath("account-generated", rel)
	}
	if rel, ok := relativeArtifactPath(b.cwd, path); ok {
		return b.capturePath("workspace", rel)
	}
	return storedArtifact{}
}

func (b *webArtifactBatch) captureUserAttachment(ref string) storedArtifact {
	if file := b.captureReference(ref); file.ID != "" {
		return file
	}
	// An explicit native input attachment identifies a file in Codex's
	// attachment store even when its name does not encode a thread ID.
	path := localArtifactPath(ref)
	if filepath.IsAbs(path) {
		if rel, inside := relativeArtifactPath(filepath.Join(b.home, "attachments"), path); inside {
			return b.capturePath("attachment", rel)
		}
	}
	return storedArtifact{}
}

// Read embedded image data only from a typed native item. The history and
// browser caches carry its registered file ID instead of the base64 bytes.
func (b *webArtifactBatch) captureImage(data string, allowRaw bool) storedArtifact {
	if header, payload, found := strings.Cut(data, ","); found && strings.HasPrefix(header, "data:image/") && strings.HasSuffix(header, ";base64") {
		data = payload
	} else if !allowRaw {
		return storedArtifact{}
	}
	if len(data) == 0 || len(data) > base64.StdEncoding.EncodedLen(inlineArtifactLimit) || !b.load() {
		return storedArtifact{}
	}
	image, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(image) > inlineArtifactLimit {
		return storedArtifact{}
	}
	mime := artifactMIME(image)
	if !strings.HasPrefix(mime, "image/") {
		return storedArtifact{}
	}
	ext := strings.TrimPrefix(mime, "image/")
	if ext == "jpeg" {
		ext = "jpg"
	}
	key := fmt.Sprintf("%x", sha256.Sum256(image))
	id := artifactKey("embedded", key)
	if saved, ok := b.byID[id]; ok {
		return saved
	}
	entry := storedArtifact{webArtifact: webArtifact{
		ID: id, Name: "image-" + key[:8] + "." + ext, Source: "attachment", MIME: mime, Size: int64(len(image)),
	}, Version: key}
	if !artifactFits(b.files, entry) || config.AtomicWrite(filepath.Join(b.dir, entry.snapshot()), image, 0600) != nil {
		return storedArtifact{}
	}
	b.embedded = append(b.embedded, entry.snapshot())
	return b.add(entry)
}

func webMarkdownArtifactReferences(body string, visit func(string) bool) {
	if !strings.ContainsAny(body, "[/`") {
		return
	}
	source := []byte(body)
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var destination []byte
		switch n := node.(type) {
		case *ast.Link:
			destination = n.Destination
		case *ast.Image:
			destination = n.Destination
		case *ast.AutoLink:
			destination = n.URL(source)
		case *ast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if s, ok := child.(*ast.Text); ok {
					destination = append(destination, s.Value(source)...)
				}
			}
			if !visit(strings.TrimSpace(string(destination))) {
				return ast.WalkStop, nil
			}
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			for remainder := n.Value(source); len(remainder) != 0; {
				match := artifactTextPath.FindSubmatchIndex(remainder)
				if match == nil {
					break
				}
				if !visit(string(remainder[match[2]:match[3]])) {
					return ast.WalkStop, nil
				}
				remainder = remainder[match[3]:]
			}
		}
		if len(destination) != 0 {
			destination = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(destination)))
			if !visit(string(destination)) {
				return ast.WalkStop, nil
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, marker := range artifactMarker.FindAllStringSubmatch(body, -1) {
		var payload struct{ Path string }
		if json.Unmarshal([]byte(marker[1]), &payload) == nil && !visit(payload.Path) {
			break
		}
	}
}

func webArtifactMessageType(raw json.RawMessage, kind string) bool {
	switch kind {
	case "imageGeneration":
		return true
	case "mcpToolCall":
		return bytes.Contains(raw, []byte(`"resource_link"`))
	default:
		return false
	}
}

type webMediaInput struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
	FileID     string `json:"fileId,omitempty"`
	ArtifactID string `json:"artifactId,omitempty"`
}

type webFileOutput struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	ArtifactIDs []string `json:"artifactIds"`
}

var generatedFileID = regexp.MustCompile(`^[0-9A-Za-z_-]{1,128}$`)

type generatedArtifactDirectory struct{ source, root, path string }

func (b *webArtifactBatch) generatedDirectories() []generatedArtifactDirectory {
	result := []generatedArtifactDirectory{{"generated", filepath.Join(b.home, "generated_images"), b.thread}}
	if !filepath.IsAbs(b.accounts) {
		return result
	}
	entries, _ := os.ReadDir(b.accounts)
	for _, account := range entries {
		if account.IsDir() && config.Nickname.MatchString(account.Name()) {
			base := filepath.Join(account.Name(), "generated_images")
			if info, err := os.Lstat(filepath.Join(b.accounts, base)); err == nil && info.IsDir() {
				result = append(result, generatedArtifactDirectory{"account-generated", b.accounts, filepath.Join(base, b.thread)})
			}
		}
	}
	return result
}

func (b *webArtifactBatch) captureGeneratedID(id string) storedArtifact {
	if !generatedFileID.MatchString(id) {
		return storedArtifact{}
	}
	for _, directory := range b.generatedDirectories() {
		for _, name := range []string{id, "ig_" + id} {
			if file := b.capturePath(directory.source, filepath.Join(directory.path, name+".png")); file.ID != "" {
				return file
			}
		}
	}
	return storedArtifact{}
}

func (b *webArtifactBatch) projectUserMedia(raw json.RawMessage, parts []json.RawMessage, remember func(storedArtifact) bool) json.RawMessage {
	changed := false
	for index, rawPart := range parts {
		var part webMediaInput
		if json.Unmarshal(rawPart, &part) != nil {
			continue
		}
		switch part.Type {
		case "image", "localImage", "audio", "localAudio", "file", "localFile", "video", "localVideo":
		default:
			continue
		}
		part.ArtifactID = ""
		file := b.captureUserAttachment(part.Path)
		if file.ID == "" && part.URL != "" {
			file = b.captureUserAttachment(part.URL)
			if file.ID == "" && part.Type == "image" {
				file = b.captureImage(part.URL, false)
			}
		}
		if file.ID == "" && part.FileID != "" && (part.Type == "image" || part.Type == "localImage") {
			file = b.captureGeneratedID(part.FileID)
		}
		if remember(file) {
			part.ArtifactID = file.ID
		}
		// A remote image can be opened by the user's browser. Filesystem and
		// embedded data URLs are provided only by the authenticated file route.
		uri, err := url.Parse(part.URL)
		if part.ArtifactID != "" || err != nil || uri.Scheme != "https" || uri.Host == "" || uri.User != nil {
			part.URL = ""
		}
		part.FileID = ""
		if len(part.Path) > 4096 {
			part.Path = ""
		}
		parts[index], _ = json.Marshal(part)
		changed = true
	}
	if !changed {
		return raw
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	fields["content"], _ = json.Marshal(parts)
	result, _ := json.Marshal(fields)
	return result
}

func (b *webArtifactBatch) projectOutput(id, kind string, raw json.RawMessage, remember func(storedArtifact) bool) json.RawMessage {
	var item struct {
		Status, SavedPath, FileID, GenerationID, Result string
	}
	output := webFileOutput{ID: id, Type: "fileOutput", ArtifactIDs: []string{}}
	add := func(file storedArtifact) {
		if remember(file) && !slices.Contains(output.ArtifactIDs, file.ID) {
			output.ArtifactIDs = append(output.ArtifactIDs, file.ID)
		}
	}
	switch kind {
	case "imageGeneration":
		if json.Unmarshal(raw, &item) != nil || (item.Status != "completed" && item.Status != "") {
			return nil
		}
		file := b.captureReference(item.SavedPath)
		if file.ID == "" {
			file = b.captureGeneratedID(item.FileID)
		}
		if file.ID == "" {
			file = b.captureGeneratedID(item.GenerationID)
		}
		if file.ID == "" && item.Result != "" {
			file = b.captureImage(item.Result, true)
		}
		add(file)
	case "mcpToolCall":
		var call struct {
			Status string
			Result struct{ Content []struct{ Type, URI string } }
		}
		if json.Unmarshal(raw, &call) != nil || call.Status != "completed" {
			return nil
		}
		for _, part := range call.Result.Content {
			if part.Type == "resource_link" {
				add(b.captureReference(part.URI))
			}
		}
		if len(output.ArtifactIDs) == 0 {
			return nil
		}
	}
	result, _ := json.Marshal(output)
	return result
}

func (b *webArtifactBatch) projectTurn(ctx context.Context, turn *WebTurn) {
	files := make(map[string]bool)
	refs := make(map[string]string)
	remember := func(file storedArtifact) bool {
		if file.ID == "" {
			return false
		}
		if !files[file.ID] {
			files[file.ID] = true
			turn.Artifacts = append(turn.Artifacts, file)
		}
		return true
	}
	items := turn.Items[:0]
	for _, raw := range turn.Items {
		var item struct {
			Type, ID, Text string
			Content        []json.RawMessage
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		switch item.Type {
		case "agentMessage":
			if ctx.Err() == nil {
				webMarkdownArtifactReferences(item.Text, func(ref string) bool {
					if len(refs) >= artifactCount || ctx.Err() != nil {
						return false
					}
					if _, seen := refs[ref]; !seen {
						file := b.captureReference(ref)
						if remember(file) {
							refs[ref] = file.ID
						}
					}
					return true
				})
			}
		case "userMessage":
			raw = b.projectUserMedia(raw, item.Content, remember)
		case "imageGeneration", "mcpToolCall":
			raw = b.projectOutput(item.ID, item.Type, raw, remember)
		}
		if len(raw) != 0 {
			items = append(items, raw)
		}
	}
	turn.Items = items
	if len(refs) != 0 {
		turn.FileRefs = refs
	}
}

func (w *WebChat) projectWebArtifacts(ctx context.Context, thread, cwd string, turns []WebTurn) {
	batch := webArtifactBatch{
		dir: filepath.Join(w.c.StateDir(), "web", "artifacts-v1", thread), home: config.Expand(w.c.Runtime.SharedCodexHome),
		accounts: config.Expand(w.c.Runtime.AccountsDir), thread: thread, cwd: cwd,
	}
	for i := range turns {
		batch.projectTurn(ctx, &turns[i])
	}
	if batch.save() != nil {
		for i := range turns {
			turns[i].Artifacts, turns[i].FileRefs = nil, nil
		}
	}
}
