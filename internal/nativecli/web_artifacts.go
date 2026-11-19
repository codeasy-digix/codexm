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
	Version string `json:"version"`
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

func registerArtifact(dir, root, source, path string, files *[]storedArtifact) error {
	file, info, err := openArtifactSource(root, path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat := info.Sys().(*syscall.Stat_t)
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(source+"\x00"+path)))
	identity := fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano())
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	entry := storedArtifact{webArtifact: webArtifact{
		ID: id, Name: filepath.Base(path), Source: source, Path: path, Size: info.Size(),
	}, Version: version}
	index := len(*files)
	var size int64 = info.Size()
	var previous string
	for i, saved := range *files {
		if saved.ID == id {
			index, previous = i, saved.snapshot()
		} else {
			size += saved.Size
		}
	}
	if previous == entry.snapshot() {
		if existing, err := os.Stat(filepath.Join(dir, previous)); err == nil && existing.Mode().IsRegular() && existing.Size() == info.Size() {
			return nil
		}
	}
	if (index == len(*files) && len(*files) >= artifactCount) || size > artifactChatLimit {
		return errors.New("이 채팅의 파일 등록 한도에 도달했습니다.")
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return errors.New("파일을 읽지 못했습니다.")
	}
	entry.MIME = "application/octet-stream"
	switch kind := http.DetectContentType(head[:n]); kind {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		entry.MIME = kind
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return errors.New("파일을 읽지 못했습니다.")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("파일 저장소를 만들지 못했습니다.")
	}
	output, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return errors.New("파일 복사본을 만들지 못했습니다.")
	}
	defer os.Remove(output.Name())
	written, copyErr := io.Copy(output, io.LimitReader(file, info.Size()+1))
	infoAfter, statErr := file.Stat()
	if copyErr != nil || written != info.Size() || statErr != nil || infoAfter.ModTime() != info.ModTime() {
		_ = output.Close()
		return errors.New("파일이 바뀌었습니다. 저장이 끝나면 다시 등록해 주세요.")
	}
	syncErr, closeErr := output.Sync(), output.Close()
	destination := filepath.Join(dir, entry.snapshot())
	if syncErr != nil || closeErr != nil || os.Rename(output.Name(), destination) != nil {
		return errors.New("파일 복사본을 저장하지 못했습니다.")
	}
	next := slices.Clone(*files)
	if index == len(next) {
		next = append(next, entry)
	} else {
		next[index] = entry
	}
	encoded, err := json.Marshal(next)
	if err != nil || config.AtomicWrite(filepath.Join(dir, "files.json"), encoded, 0600) != nil {
		if entry.snapshot() != previous {
			_ = os.Remove(destination)
		}
		return errors.New("등록한 파일 목록을 저장하지 못했습니다.")
	}
	*files = next
	if previous != "" && previous != entry.snapshot() {
		_ = os.Remove(filepath.Join(dir, previous))
	}
	return nil
}

func scanVisualizations(ctx context.Context, root, thread, dir string, files *[]storedArtifact) {
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
			if ctx.Err() != nil || len(*files) >= artifactCount {
				return fs.SkipAll
			}
			if err != nil {
				return nil
			}
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			if entry.Type().IsRegular() {
				_ = registerArtifact(dir, root, "visualization", path, files)
			}
			return nil
		})
	}
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
	files, err := loadArtifacts(dir)
	if err != nil {
		return nil, err
	}
	switch method {
	case "web.artifacts":
		scanVisualizations(ctx, filepath.Join(home, "visualizations"), p.Thread, dir, &files)
	case "web.artifactRegister":
		path := strings.TrimSpace(p.FilePath)
		if !filepath.IsAbs(entry.Cwd) || !artifactPath(path) {
			return nil, errors.New("작업 폴더 기준의 파일 경로를 입력해 주세요.")
		}
		if err := registerArtifact(dir, entry.Cwd, "workspace", filepath.Clean(path), &files); err != nil {
			return nil, err
		}
	case "web.artifact":
		if !artifactID.MatchString(p.Artifact) {
			return nil, nil
		}
		for _, file := range files {
			if file.ID == p.Artifact && artifactID.MatchString(file.Version) {
				return file, nil
			}
		}
		return nil, nil
	}
	return files, nil
}
