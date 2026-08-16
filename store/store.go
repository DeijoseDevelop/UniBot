package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

// TokenStore implementa googleauth.TokenStore sobre PostgreSQL (Supabase).
type TokenStore struct {
	pool *pgxpool.Pool
}

// New conecta a PostgreSQL, crea las tablas y devuelve el TokenStore.
func New(ctx context.Context, databaseURL string) (*TokenStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &TokenStore{pool: pool}, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS tokens (
	user_id       BIGINT PRIMARY KEY,
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	token_type    TEXT NOT NULL DEFAULT 'Bearer',
	expiry        TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS conversations (
	user_id    BIGINT PRIMARY KEY,
	history    JSONB NOT NULL DEFAULT '[]'::jsonb,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`)
	return err
}

// GetTokens devuelve el token OAuth2 almacenado para un usuario.
func (s *TokenStore) GetTokens(ctx context.Context, userID int64) (*oauth2.Token, error) {
	var tok oauth2.Token
	var expiry *time.Time

	err := s.pool.QueryRow(ctx,
		`SELECT access_token, refresh_token, token_type, expiry
		   FROM tokens WHERE user_id = $1`,
		userID,
	).Scan(&tok.AccessToken, &tok.RefreshToken, &tok.TokenType, &expiry)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("token no encontrado")
	}
	if err != nil {
		return nil, err
	}
	if expiry != nil {
		tok.Expiry = *expiry
	}
	return &tok, nil
}

// SaveTokens guarda o actualiza el token OAuth2 de un usuario.
func (s *TokenStore) SaveTokens(ctx context.Context, userID int64, token *oauth2.Token) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO tokens (user_id, access_token, refresh_token, token_type, expiry)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE SET
	access_token  = EXCLUDED.access_token,
	refresh_token = EXCLUDED.refresh_token,
	token_type    = EXCLUDED.token_type,
	expiry        = EXCLUDED.expiry`,
		userID, token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
	return err
}

// RevokeTokens elimina los tokens de un usuario (comando /revoke).
func (s *TokenStore) RevokeTokens(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tokens WHERE user_id = $1`, userID)
	return err
}

// Close cierra el pool de conexiones.
func (s *TokenStore) Close() {
	s.pool.Close()
}
