// Package secretbox encrypts small secrets at rest (provider credentials,
// integration signing secrets) with AES-256-GCM under BRIDGE_SECRET_KEY.
//
// A sealed value is version byte 1, a 12-byte random nonce, then the
// ciphertext and tag. The row's ID is bound as additional data, so a sealed
// value copied to another row does not open.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

const version = 1

// ErrNoKey means BRIDGE_SECRET_KEY is not set.
var ErrNoKey = errors.New("BRIDGE_SECRET_KEY is not set")

// ErrOpen means a value could not be decrypted: a different key, or a value
// moved between rows.
var ErrOpen = errors.New("could not decrypt the stored secret; was BRIDGE_SECRET_KEY changed?")

// Box seals and opens values. The zero Box (no key) refuses both.
type Box struct{ aead cipher.AEAD }

// New returns a Box for a 32-byte key; a nil key gives a Box that refuses to work.
func New(key []byte) (*Box, error) {
	if key == nil {
		return &Box{}, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Ready reports whether a key is configured.
func (b *Box) Ready() bool { return b != nil && b.aead != nil }

// Seal encrypts plaintext, bound to id.
func (b *Box) Seal(id string, plaintext []byte) ([]byte, error) {
	if !b.Ready() {
		return nil, ErrNoKey
	}
	out := make([]byte, 1+b.aead.NonceSize(), 1+b.aead.NonceSize()+len(plaintext)+b.aead.Overhead())
	out[0] = version
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, err
	}
	return b.aead.Seal(out, out[1:], plaintext, []byte(id)), nil
}

// Open decrypts a value sealed for id.
func (b *Box) Open(id string, sealed []byte) ([]byte, error) {
	if !b.Ready() {
		return nil, ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != version {
		return nil, ErrOpen
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(id))
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
