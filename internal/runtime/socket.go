package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func SocketPath(c config.Config, name string) string {
	root := c.StateDir()
	if name != "control" {
		root = filepath.Join(root, "sockets")
	}
	p := filepath.Join(root, name+".sock")
	if len(p) < 100 {
		return p
	}
	h := sha256.Sum256([]byte(c.StateDir()))
	root = filepath.Join("/tmp", fmt.Sprintf("codexm-%d-%s", os.Getuid(), hex.EncodeToString(h[:8])))
	return filepath.Join(root, name+".sock")
}
func PrivateSocketDir(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	var st unix.Stat_t
	if e := unix.Lstat(dir, &st); e != nil {
		return e
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || int(st.Uid) != os.Getuid() || st.Mode&077 != 0 {
		return fmt.Errorf("socket directory must be private and owned by this user: %s", dir)
	}
	return nil
}
