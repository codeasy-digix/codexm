package nativecli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func webDirectories(path string) (any, error) {
	if path == "" {
		path = webDefaultCwd()
	}
	directory, err := webDirectory(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, errors.New("이 폴더의 목록을 읽을 수 없습니다.")
	}
	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		isDirectory := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(directory, entry.Name()))
			isDirectory = err == nil && info.IsDir()
		}
		if isDirectory {
			children = append(children, entry.Name())
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		return strings.ToLower(children[i]) < strings.ToLower(children[j])
	})
	parent := filepath.Dir(directory)
	if parent == directory {
		parent = ""
	}
	return map[string]any{"path": directory, "parent": parent, "directories": children}, nil
}
