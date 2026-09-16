package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

type Store struct {
	Dir string
}

func New(dataDir string) Store {
	return Store{Dir: filepath.Join(dataDir, "ssh")}
}

func (s Store) PrivateKeyPath() string { return filepath.Join(s.Dir, "id_ed25519") }
func (s Store) PublicKeyPath() string  { return filepath.Join(s.Dir, "id_ed25519.pub") }
func (s Store) KnownHostsPath() string { return filepath.Join(s.Dir, "known_hosts") }

func (s Store) Exists() bool {
	_, err := os.Stat(s.PrivateKeyPath())
	return err == nil
}

func (s Store) PublicKey() (string, error) {
	data, err := os.ReadFile(s.PublicKeyPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s Store) EnsureExists(comment string) (bool, error) {
	if s.Exists() {
		return false, nil
	}
	return true, s.Generate(comment)
}

func (s Store) Generate(comment string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}
	return s.write(pem.EncodeToMemory(block), sshPub, comment)
}

func (s Store) Import(privateKeyPEM []byte) error {
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return errors.New("the key is protected by a passphrase; only keys without passphrase can be used")
		}
		return fmt.Errorf("invalid private key: %w", err)
	}
	return s.write(privateKeyPEM, signer.PublicKey(), "database-dumper")
}

func (s Store) write(privateKeyPEM []byte, pub ssh.PublicKey, comment string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(s.PrivateKeyPath(), privateKeyPEM, 0o600); err != nil {
		return err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	if err := os.WriteFile(s.PublicKeyPath(), []byte(line+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(s.KnownHostsPath()); os.IsNotExist(err) {
		if err := os.WriteFile(s.KnownHostsPath(), nil, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) Fingerprint() (string, error) {
	data, err := os.ReadFile(s.PublicKeyPath())
	if err != nil {
		return "", err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(pub), nil
}

func (s Store) SSHArgs() []string {
	if !s.Exists() {
		return nil
	}
	return []string{
		"-i", s.PrivateKeyPath(),
		"-o", "UserKnownHostsFile=" + s.KnownHostsPath(),
		"-o", "StrictHostKeyChecking=accept-new",
	}
}
