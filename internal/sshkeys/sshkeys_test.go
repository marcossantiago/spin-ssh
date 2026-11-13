package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
)

// writeKey writes an ed25519 private key (encrypted when passphrase is set) and returns
// its path and authorized_keys line.
func writeKey(t *testing.T, dir, name, passphrase string) (string, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	return path, string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
}

func TestPublicKeyOfEncryptedFileNeedsNoPassphrase(t *testing.T) {
	path, line := writeKey(t, t.TempDir(), "id_ed25519", "secret")
	pub, err := PublicKeyOfPrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ssh.MarshalAuthorizedKey(pub)); got != line {
		t.Fatalf("public key = %q, want %q", got, line)
	}
}

func TestMatchTeamAndVerify(t *testing.T) {
	dir := t.TempDir()
	minePath, mine := writeKey(t, dir, "mine", "")
	otherPath, other := writeKey(t, dir, "other", "")
	team := []spinupwp.SSHKey{
		{ID: 1, Name: "Someone", PublicKey: other + " someone@laptop"},
		{ID: 2, Name: "Me", PublicKey: mine},
	}

	pub, _ := PublicKeyOfPrivateFile(minePath)
	matches := MatchTeam([]Local{{Key: pub, PrivateKeyFile: minePath}}, team)
	if len(matches) != 1 || matches[0].Team.ID != 2 {
		t.Fatalf("matches = %+v", matches)
	}

	if _, err := Verify(minePath, team[1]); err != nil {
		t.Fatalf("Verify(mine) = %v", err)
	}
	if _, err := Verify(otherPath, team[1]); err == nil {
		t.Fatal("Verify accepted the wrong key")
	}
}

func TestIdentityFileForAgentKey(t *testing.T) {
	_, line := writeKey(t, t.TempDir(), "k", "")
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(line))
	path, err := IdentityFile(Local{Key: pub, InAgent: true}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != line || filepath.Ext(path) != ".pub" {
		t.Fatalf("wrote %s: %q", path, data)
	}
}
