// Package sshkeys finds the user's local SSH keys (files in ~/.ssh and keys in the SSH
// agent that ssh would use) and matches them against the SpinupWP team's keys.
package sshkeys

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
)

// Local is a key the user can authenticate with.
type Local struct {
	Key ssh.PublicKey
	// PrivateKeyFile is set when the private key is a file on disk.
	PrivateKeyFile string
	// InAgent is true when the agent ssh uses holds the key.
	InAgent bool
	Comment string
}

// Fingerprint is the SHA256 fingerprint, as ssh-keygen -l prints it.
func (l Local) Fingerprint() string { return ssh.FingerprintSHA256(l.Key) }

// AuthorizedKey is the key as one authorized_keys line, without a comment.
func (l Local) AuthorizedKey() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(l.Key)))
}

// Describe is a short human label: where the key lives.
func (l Local) Describe() string {
	switch {
	case l.PrivateKeyFile != "" && l.InAgent:
		return tildify(l.PrivateKeyFile) + " (also in agent)"
	case l.PrivateKeyFile != "":
		return tildify(l.PrivateKeyFile)
	case l.Comment != "":
		return "agent: " + l.Comment
	default:
		return "agent"
	}
}

// Match is a local key that is also on the SpinupWP team.
type Match struct {
	Local Local
	Team  spinupwp.SSHKey
}

// Discover returns the private keys in ~/.ssh and the keys in the SSH agent, merged by
// public key. Unreadable files are skipped: this is best effort.
func Discover() ([]Local, error) {
	var keys []Local
	index := map[string]int{}
	add := func(l Local) {
		id := string(l.Key.Marshal())
		if i, ok := index[id]; ok {
			if l.PrivateKeyFile != "" && keys[i].PrivateKeyFile == "" {
				keys[i].PrivateKeyFile = l.PrivateKeyFile
			}
			keys[i].InAgent = keys[i].InAgent || l.InAgent
			if keys[i].Comment == "" {
				keys[i].Comment = l.Comment
			}
			return
		}
		index[id] = len(keys)
		keys = append(keys, l)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".ssh"))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		path := filepath.Join(home, ".ssh", e.Name())
		if pub, err := PublicKeyOfPrivateFile(path); err == nil {
			add(Local{Key: pub, PrivateKeyFile: path})
		}
	}

	if sock := agentSocket(); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			list, _ := agent.NewClient(conn).List()
			conn.Close()
			for _, k := range list {
				pub, err := ssh.ParsePublicKey(k.Marshal())
				if err != nil {
					continue
				}
				add(Local{Key: pub, InAgent: true, Comment: k.Comment})
			}
		}
	}
	return keys, nil
}

// MatchTeam returns the local keys that are on the team, in local order.
func MatchTeam(local []Local, team []spinupwp.SSHKey) []Match {
	var matches []Match
	for _, l := range local {
		for _, t := range team {
			if pub, err := parseAuthorized(t.PublicKey); err == nil && bytes.Equal(pub.Marshal(), l.Key.Marshal()) {
				matches = append(matches, Match{Local: l, Team: t})
				break
			}
		}
	}
	return matches
}

// PublicKeyOfPrivateFile reads the public half of a private key file without needing
// its passphrase. OpenSSH-format keys store it in the clear; for older encrypted PEM
// keys it falls back to the .pub file next to it.
func PublicKeyOfPrivateFile(path string) (ssh.PublicKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 64<<10 {
		return nil, errors.New("too large to be a private key")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(data, []byte("PRIVATE KEY")) {
		return nil, errors.New("not a private key")
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer.PublicKey(), nil
	}
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) && missing.PublicKey != nil {
		return missing.PublicKey, nil
	}
	if pubData, pubErr := os.ReadFile(path + ".pub"); pubErr == nil {
		if pub, pubErr := parseAuthorized(string(pubData)); pubErr == nil {
			return pub, nil
		}
	}
	return nil, err
}

// Verify checks that a private key file the user typed is the team key they picked.
func Verify(path string, team spinupwp.SSHKey) (Local, error) {
	pub, err := PublicKeyOfPrivateFile(path)
	if err != nil {
		return Local{}, fmt.Errorf("can't read %s: %w", tildify(path), err)
	}
	want, err := parseAuthorized(team.PublicKey)
	if err != nil {
		return Local{}, fmt.Errorf("can't parse team key %q: %w", team.Name, err)
	}
	if !bytes.Equal(pub.Marshal(), want.Marshal()) {
		return Local{}, fmt.Errorf("%s is %s, not the team key %q", tildify(path), ssh.FingerprintSHA256(pub), team.Name)
	}
	return Local{Key: pub, PrivateKeyFile: path}, nil
}

// IdentityFile returns the path to give ssh -i. For an agent-only key it writes the
// public key under dir, and ssh uses the agent's matching private key.
func IdentityFile(l Local, dir string) (string, error) {
	if l.PrivateKeyFile != "" {
		return l.PrivateKeyFile, nil
	}
	sum := sha256.Sum256(l.Key.Marshal())
	path := filepath.Join(dir, "agent-"+hex.EncodeToString(sum[:6])+".pub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, ssh.MarshalAuthorizedKey(l.Key), 0o600)
}

// ExpandHome turns a leading ~/ into the home directory.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// agentSocket is the agent ssh itself would use: IdentityAgent from ssh config (how
// 1Password and Secretive are usually set up), else SSH_AUTH_SOCK.
func agentSocket() string {
	if out, err := exec.Command("ssh", "-G", "spin-ssh.invalid").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			name, value, ok := strings.Cut(strings.TrimSpace(line), " ")
			if !ok || name != "identityagent" {
				continue
			}
			value = strings.Trim(value, `"`)
			switch value {
			case "none":
				return ""
			case "SSH_AUTH_SOCK", "$SSH_AUTH_SOCK":
			default:
				return ExpandHome(os.ExpandEnv(value))
			}
		}
	}
	return os.Getenv("SSH_AUTH_SOCK")
}

func parseAuthorized(line string) (ssh.PublicKey, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	return pub, err
}

func tildify(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
