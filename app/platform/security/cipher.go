package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// 与 chenyme Cipher 对齐：AES-256-GCM + Base64 密钥；
// 密文前缀 gorkenc:v1: 便于明文兼容读（无前缀当明文）。
const encryptedPrefix = "gorkenc:v1:"

// Cipher 使用 AES-256-GCM 加密敏感凭据。
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 从 Base64 编码的 32 字节密钥创建加密器。
func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("parse credential encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("credential encryption key must be base64-encoded 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// OpenCipher 空密钥返回 (nil, nil) 表示加密关闭；非空则校验并创建。
func OpenCipher(encodedKey string) (*Cipher, error) {
	if strings.TrimSpace(encodedKey) == "" {
		return nil, nil
	}
	return NewCipher(encodedKey)
}

// Encrypt 加密明文；空串原样返回。返回带 gorkenc:v1: 前缀的可落库字符串。
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if c == nil {
		return plaintext, nil
	}
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密。无前缀时按明文兼容返回（存量未加密数据）。
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if !strings.HasPrefix(encoded, encryptedPrefix) {
		// 明文兼容读：历史数据无前缀，原样返回。
		return encoded, nil
	}
	if c == nil {
		return "", fmt.Errorf("encrypted credential present but encryption key is not configured")
	}
	raw := strings.TrimPrefix(encoded, encryptedPrefix)
	data, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("parse encrypted credential: %w", err)
	}
	if len(data) < c.aead.NonceSize() {
		return "", fmt.Errorf("encrypted credential length invalid")
	}
	nonce, ciphertext := data[:c.aead.NonceSize()], data[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted 判断字符串是否为本方案密文。
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}

// SealOptional 有 cipher 则加密，否则原样；用于可选启用路径。
func SealOptional(c *Cipher, plaintext string) (string, error) {
	if c == nil {
		return plaintext, nil
	}
	return c.Encrypt(plaintext)
}

// OpenOptional 有 cipher 则走 Decrypt（含明文兼容），否则原样。
func OpenOptional(c *Cipher, value string) (string, error) {
	if c == nil {
		if IsEncrypted(value) {
			return "", fmt.Errorf("encrypted credential present but encryption key is not configured")
		}
		return value, nil
	}
	return c.Decrypt(value)
}
