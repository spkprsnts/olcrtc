package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(keyStr string) (*Cipher, error) {
	key := []byte(keyStr)
	if len(key) != chacha20poly1305.KeySize {
		return nil, errors.New("invalid key size")
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:c.aead.NonceSize()]
	encrypted := ciphertext[c.aead.NonceSize():]

	return c.aead.Open(nil, nonce, encrypted, nil)
}
