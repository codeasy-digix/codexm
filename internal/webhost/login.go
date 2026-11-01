package webhost

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/term"
)

type loginRecord struct {
	Username   string `json:"username"`
	Algorithm  string `json:"algorithm"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

func saveLogin(state, username, password string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`).MatchString(username) {
		return errors.New("username must contain 1–64 letters, numbers, or _.@-")
	}
	if len(password) < 8 || len(password) > 1024 || strings.ContainsAny(password, "\r\n\x00") {
		return errors.New("password must contain 8–1024 bytes without newlines")
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return err
	}
	b, err := json.Marshal(loginRecord{username, "pbkdf2-sha256", 600000, hex.EncodeToString(salt), hex.EncodeToString(hash)})
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(state, "login.json"), b, 0600)
}

func configureLogin(state, username string, stdin bool, out io.Writer) error {
	var password []byte
	var err error
	if stdin {
		password, err = io.ReadAll(io.LimitReader(os.Stdin, 4097))
		password = []byte(strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r"))
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("use --password-stdin when no terminal is attached")
		}
		fmt.Fprint(out, "Web password: ")
		password, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		if err == nil {
			fmt.Fprint(out, "Repeat password: ")
			var confirm []byte
			confirm, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(out)
			if err == nil && string(password) != string(confirm) {
				return errors.New("passwords do not match")
			}
		}
	}
	if err != nil {
		return err
	}
	if err = saveLogin(state, username, string(password)); err != nil {
		return err
	}
	fmt.Fprintln(out, "Web login saved. Previous browser sessions are revoked; native Codex accounts are unchanged.")
	return nil
}
