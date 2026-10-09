package mobile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type vault struct {
	dir  string
	aead cipher.AEAD
	mu   sync.Mutex
}

func newVault(dir string, key []byte) (*vault, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid storage key")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	return &vault{dir: dir, aead: a}, nil
}
func (v *vault) filename(name string) string {
	h := sha256.Sum256([]byte(name))
	return filepath.Join(v.dir, hex.EncodeToString(h[:])+".enc")
}
func (v *vault) read(name string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, e := os.ReadFile(v.filename(name))
	if e != nil {
		return nil, e
	}
	if len(b) < v.aead.NonceSize() {
		return nil, errors.New("damaged encrypted storage")
	}
	return v.aead.Open(nil, b[:v.aead.NonceSize()], b[v.aead.NonceSize():], []byte(name))
}
func (v *vault) write(name string, data []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if data == nil {
		e := os.Remove(v.filename(name))
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, nonce); e != nil {
		return e
	}
	enc := v.aead.Seal(nonce, nonce, data, []byte(name))
	f, e := os.CreateTemp(v.dir, ".encrypted-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(enc)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return replaceVaultFile(tmp, v.filename(name))
}
