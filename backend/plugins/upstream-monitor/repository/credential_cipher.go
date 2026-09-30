package repository

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/Wei-Shaw/sub2api/ent"
)

// CredentialCipher encrypts plugin-owned upstream credentials with a key
// persisted by the plugin itself. It deliberately does not depend on the
// host TOTP/JWT configuration, so restarting the host cannot invalidate
// upstream monitor credentials.
type CredentialCipher struct {
	client *ent.Client
}

func NewCredentialCipher(client *ent.Client) *CredentialCipher {
	return &CredentialCipher{client: client}
}

func (c *CredentialCipher) Encrypt(ctx context.Context, plaintext string) (string, error) {
	key, err := c.key(ctx)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *CredentialCipher) Decrypt(ctx context.Context, value string) (string, error) {
	key, err := c.key(ctx)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode credential: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("credential ciphertext is too short")
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (c *CredentialCipher) key(ctx context.Context) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("plugin credential cipher is not initialized")
	}
	rows, err := c.client.QueryContext(ctx, `SELECT encryption_key FROM upstream_monitor_secrets WHERE id = 1`)
	if err != nil {
		return nil, fmt.Errorf("load plugin credential key: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("load plugin credential key: %w", err)
		}
		return nil, errors.New("plugin credential key is not initialized")
	}
	var encoded string
	if err := rows.Scan(&encoded); err != nil {
		return nil, fmt.Errorf("load plugin credential key: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("plugin credential key is invalid")
	}
	return key, nil
}
