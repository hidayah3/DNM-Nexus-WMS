package internal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresTokenStore struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

func NewPostgresTokenStore(pool *pgxpool.Pool, base64Key string) (*PostgresTokenStore, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil || len(key) != 32 {
		return nil, errors.New("WMS_TOKEN_ENCRYPTION_KEY must be base64 for exactly 32 random bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &PostgresTokenStore{pool: pool, aead: aead}, nil
}

func (s *PostgresTokenStore) Load(ctx context.Context) (OAuthTokens, error) {
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, `SELECT encrypted_tokens FROM wms_provider_auth WHERE provider='easyparcel'`).Scan(&ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthTokens{}, ErrNoEasyParcelConnection
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	nonceSize := s.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return OAuthTokens{}, errors.New("stored EasyParcel credentials are invalid")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.aead.Open(nil, nonce, sealed, []byte("easyparcel-oauth-v1"))
	if err != nil {
		return OAuthTokens{}, errors.New("stored EasyParcel credentials could not be decrypted; check the encryption key")
	}
	return unmarshalOAuthTokens(plaintext)
}

func (s *PostgresTokenStore) Save(ctx context.Context, tokens OAuthTokens) error {
	plaintext, err := marshalOAuthTokens(tokens)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := append(nonce, s.aead.Seal(nil, nonce, plaintext, []byte("easyparcel-oauth-v1"))...)
	_, err = s.pool.Exec(ctx, `INSERT INTO wms_provider_auth(provider,encrypted_tokens,updated_at)
		VALUES('easyparcel',$1,now()) ON CONFLICT(provider) DO UPDATE SET encrypted_tokens=EXCLUDED.encrypted_tokens,updated_at=now()`, ciphertext)
	return err
}

func (s *PostgresTokenStore) Delete(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM wms_provider_auth WHERE provider='easyparcel'`)
	return err
}
