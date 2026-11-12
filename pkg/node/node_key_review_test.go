package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRLPxIdentityPersistsAndRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodekey")
	first, err := loadNodeKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadNodeKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.PublicKey.Equal(&second.PublicKey) {
		t.Fatal("identity changed on reopen")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("private identity file permissions are not 0600")
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNodeKey(path); err == nil {
		t.Fatal("corrupt identity was silently replaced")
	}
}
