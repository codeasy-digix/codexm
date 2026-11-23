package nativecli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
)

const (
	artifactFileLimit = 128 * 1024 * 1024
	artifactChatLimit = 512 * 1024 * 1024
	artifactCount     = 128
)

var artifactID = regexp.MustCompile(`^[0-9a-f]{64}$`)

type webArtifact struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
}

type storedArtifact struct {
	webArtifact
	Version  string `json:"version"`
	Deferred bool   `json:"deferred,omitempty"`
}

func (a storedArtifact) snapshot() string { return a.ID + "-" + a.Version }

func artifactPath(path string) bool {
	if path == "" || len(path) > 4096 || !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || (strings.HasPrefix(part, ".") && part != ".") || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return false
		}
	}
	return filepath.Clean(path) != "."
}

// Open each path component relative to the preceding directory descriptor.
// A symlink cannot redirect a registered file, even if the path changes mid-read.
func openArtifactSource(root, path string) (*os.File, os.FileInfo, error) {
	if !artifactPath(path) {
		return nil, nil, errors.New("작업 폴더 기준의 파일 경로를 입력해 주세요.")
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("작업 폴더를 열 수 없습니다.")
	}
	parts := strings.Split(filepath.Clean(path), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, nil, errors.New("파일을 열 수 없습니다. 파일 경로와 접근 권한을 확인해 주세요.")
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > artifactFileLimit {
		_ = file.Close()
		return nil, nil, errors.New("128 MiB 이하의 일반 파일을 등록할 수 있습니다.")
	}
	return file, info, nil
}

func loadArtifacts(dir string) ([]storedArtifact, error) {
	bytes, err := os.ReadFile(filepath.Join(dir, "files.json"))
	if os.IsNotExist(err) {
		return []storedArtifact{}, nil
	}
	var files []storedArtifact
	if err != nil || json.Unmarshal(bytes, &files) != nil || len(files) > artifactCount {
		return nil, errors.New("등록한 파일 목록을 읽지 못했습니다.")
	}
	return files, nil
}

func artifactVersion(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	identity := fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano())
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func artifactKey(source, path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(source+"\x00"+path)))
}

func artifactMIME(head []byte) string {
	switch kind := http.DetectContentType(head); kind {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "video/webm":
		return kind
	default:
		return "application/octet-stream"
	}
}

func describeArtifact(file *os.File, info os.FileInfo, source, path string) (storedArtifact, error) {
	var head [512]byte
	n, err := file.ReadAt(head[:], 0)
	if err != nil && err != io.EOF {
		return storedArtifact{}, errors.New("파일을 읽지 못했습니다.")
	}
	return storedArtifact{webArtifact: webArtifact{
		ID: artifactKey(source, path), Name: filepath.Base(path), Source: source, Path: path, Size: info.Size(), MIME: artifactMIME(head[:n]),
	}, Version: artifactVersion(info)}, nil
}

func artifactFits(files []storedArtifact, entry storedArtifact) bool {
	count, size := 1, entry.Size
	for _, saved := range files {
		if saved.ID != entry.ID {
			count++
			size += saved.Size
		}
	}
	return count <= artifactCount && size <= artifactChatLimit
}

func saveArtifacts(dir string, files []storedArtifact) error {
	encoded, err := json.Marshal(files)
	if err != nil || config.AtomicWrite(filepath.Join(dir, "files.json"), encoded, 0600) != nil {
		return errors.New("등록한 파일 목록을 저장하지 못했습니다.")
	}
	return nil
}

func writeArtifactSnapshot(ctx context.Context, dir string, file *os.File, entry storedArtifact) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("파일 저장소를 만들지 못했습니다.")
	}
	output, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return errors.New("파일 복사본을 만들지 못했습니다.")
	}
	defer os.Remove(output.Name())
	written, copyErr := io.Copy(output, io.LimitReader(file, entry.Size+1))
	infoAfter, statErr := file.Stat()
	if ctx.Err() != nil || copyErr != nil || written != entry.Size || statErr != nil || artifactVersion(infoAfter) != entry.Version {
		_ = output.Close()
		return errors.New("파일이 바뀌었습니다. 저장이 끝나면 다시 등록해 주세요.")
	}
	syncErr, closeErr := output.Sync(), output.Close()
	destination := filepath.Join(dir, entry.snapshot())
	if syncErr != nil || closeErr != nil || os.Rename(output.Name(), destination) != nil {
		return errors.New("파일 복사본을 저장하지 못했습니다.")
	}
	return nil
}

func registerArtifact(ctx context.Context, dir, root, source, path string, files *[]storedArtifact) error {
	file, info, err := openArtifactSource(root, path)
	if err != nil {
		return err
	}
	defer file.Close()
	entry, err := describeArtifact(file, info, source, path)
	if err != nil {
		return err
	}
	index := len(*files)
	var previous string
	if i := slices.IndexFunc(*files, func(saved storedArtifact) bool { return saved.ID == entry.ID }); i >= 0 {
		index, previous = i, (*files)[i].snapshot()
		if previous == entry.snapshot() && !(*files)[i].Deferred && (*files)[i].MIME == entry.MIME {
			if existing, err := os.Stat(filepath.Join(dir, previous)); err == nil && existing.Mode().IsRegular() && existing.Size() == entry.Size {
				return nil
			}
		}
	}
	if !artifactFits(*files, entry) {
		return errors.New("이 채팅의 파일 등록 한도에 도달했습니다.")
	}
	if err := writeArtifactSnapshot(ctx, dir, file, entry); err != nil {
		return err
	}
	next := slices.Clone(*files)
	if index == len(next) {
		next = append(next, entry)
	} else {
		next[index] = entry
	}
	if err := saveArtifacts(dir, next); err != nil {
		if entry.snapshot() != previous {
			_ = os.Remove(filepath.Join(dir, entry.snapshot()))
		}
		return err
	}
	*files = next
	if previous != "" && previous != entry.snapshot() {
		_ = os.Remove(filepath.Join(dir, previous))
	}
	return nil
}

func scanVisualizations(ctx context.Context, root, thread string, visit func(string) bool) {
	source, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer source.Close()
	dates, err := fs.Glob(source.FS(), "[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]/"+thread)
	if err != nil {
		return
	}
	for _, folder := range dates {
		_ = fs.WalkDir(source.FS(), folder, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return fs.SkipAll
			}
			if err != nil {
				return nil
			}
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			if entry.Type().IsRegular() && !visit(path) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

// A discovered path is checked and indexed during history reads. Copy its
// bytes only when a browser requests it. Reopening must match the observed file.
func materializeArtifact(ctx context.Context, dir, root string, files []storedArtifact, index int) error {
	entry := files[index]
	file, info, err := openArtifactSource(root, entry.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	if artifactVersion(info) != entry.Version {
		return errors.New("등록한 파일이 변경되었습니다.")
	}
	if err := writeArtifactSnapshot(ctx, dir, file, entry); err != nil {
		return err
	}
	files[index].Deferred = false
	return saveArtifacts(dir, files)
}

func (w *WebChat) artifacts(ctx context.Context, method string, p webParams) (any, error) {
	if !threadID.MatchString(p.Thread) {
		return nil, errors.New("파일이 속한 채팅을 확인할 수 없습니다.")
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	entry, err := catalog.Lookup(ctx, home, p.Thread)
	if errors.Is(err, sql.ErrNoRows) {
		// A web-created draft has no native row until its first turn.
		draft, draftErr := w.thread(ctx, p.Thread)
		if draftErr == nil && draft != nil {
			entry.Cwd, err = draft.Cwd, nil
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		if method == "web.artifacts" {
			return []webArtifact{}, nil
		}
		return nil, errors.New("파일이 속한 채팅을 찾을 수 없습니다.")
	}
	if err != nil {
		return nil, errors.New("파일이 속한 채팅을 확인할 수 없습니다.")
	}
	dir := filepath.Join(w.c.StateDir(), "web", "artifacts-v1", p.Thread)
	batch := webArtifactBatch{dir: dir, home: home, accounts: config.Expand(w.c.Runtime.AccountsDir), cwd: entry.Cwd, thread: p.Thread}
	if !batch.load() {
		return nil, batch.err
	}
	switch method {
	case "web.artifacts":
		scanVisualizations(ctx, filepath.Join(home, "visualizations"), p.Thread, func(path string) bool {
			batch.capturePath("visualization", path)
			return len(batch.files) < artifactCount
		})
		for _, directory := range batch.generatedDirectories() {
			path := filepath.Join(directory.root, directory.path)
			if info, err := os.Lstat(path); err != nil || !info.IsDir() {
				continue
			}
			entries, _ := os.ReadDir(path)
			for _, entry := range entries {
				if ctx.Err() != nil || len(batch.files) >= artifactCount {
					break
				}
				if entry.Type().IsRegular() {
					batch.capturePath(directory.source, filepath.Join(directory.path, entry.Name()))
				}
			}
		}
		if err := batch.save(); err != nil {
			return nil, err
		}
	case "web.artifactRegister":
		path := strings.TrimSpace(p.FilePath)
		if !filepath.IsAbs(entry.Cwd) || !artifactPath(path) {
			return nil, errors.New("작업 폴더 기준의 파일 경로를 입력해 주세요.")
		}
		if err := registerArtifact(ctx, dir, entry.Cwd, "workspace", filepath.Clean(path), &batch.files); err != nil {
			return nil, err
		}
	case "web.artifact":
		if !artifactID.MatchString(p.Artifact) {
			return nil, nil
		}
		for index, file := range batch.files {
			if file.ID == p.Artifact && artifactID.MatchString(file.Version) {
				if file.Deferred {
					root := batch.root(file.Source, file.Path)
					if root == "" || materializeArtifact(ctx, dir, root, batch.files, index) != nil {
						return nil, nil
					}
				}
				return batch.files[index], nil
			}
		}
		return nil, nil
	}
	return batch.files, nil
}
