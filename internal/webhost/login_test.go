package webhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginIsPrivateHashedAndRotates(t *testing.T) {
	dir := t.TempDir()
	if err := saveLogin(dir, "fixture", "fixture-password"); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "login.json")
	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "fixture-password") {
		t.Fatal("plaintext password persisted")
	}
	info, _ := os.Stat(filename)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credentials are not private")
	}
	var before, after loginRecord
	if err = json.Unmarshal(b, &before); err != nil {
		t.Fatal(err)
	}
	if err = saveLogin(dir, "fixture", "fixture-password"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filename)
	_ = json.Unmarshal(b, &after)
	if before.Hash == after.Hash || before.Salt == after.Salt {
		t.Fatal("credential rotation reused salt")
	}
	if err = saveLogin(dir, "bad\nuser", "fixture-password"); err == nil {
		t.Fatal("invalid username accepted")
	}
}

func TestPublicOriginCannotExposeTheMeteorListener(t *testing.T) {
	for _, origin := range []string{"http://example.com", "https://u:p@example.com", "https://example.com/a", "https://example.com?x=1", "https://example.com#x", "https://example.com:99999"} {
		if _, err := httpsOrigin(origin); err == nil {
			t.Fatal("unsafe origin accepted", origin)
		}
	}
	value, err := httpsOrigin("https://203.0.113.10:8443/")
	if err != nil || value != "https://203.0.113.10:8443" {
		t.Fatal(value, err)
	}
	for _, address := range []string{"0.0.0.0:4317", "192.0.2.12:4317", "127.0.0.1:443"} {
		if _, err = listenAddress(Spec{Origin: value, Listen: address}); err == nil {
			t.Fatal("public listener accepted", address)
		}
	}
	if _, err = listenAddress(Spec{Origin: value, Listen: "127.0.0.1:4317"}); err != nil {
		t.Fatal(err)
	}
}
