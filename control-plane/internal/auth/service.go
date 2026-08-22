package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Claims are the JWT claims carried by access tokens.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// Service implements user registration, login, and session management.
type Service struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
}

// Indirection seams so tests can inject token-minting failures; production
// always uses the real implementations assigned here.
var (
	issueAccessTokenFn = (*Service).issueAccessToken
	randomTokenFn      = randomToken
)

// NewService returns a Service backed by the given pool and JWT secret.
func NewService(pool *pgxpool.Pool, jwtSecret string) *Service {
	return &Service{pool: pool, jwtSecret: []byte(jwtSecret)}
}

// Register creates a user with a bcrypt-hashed password and returns its ID.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	var id string
	err = s.pool.QueryRow(ctx, `
		insert into users(email, password_hash, display_name)
		values ($1, $2, $3)
		returning id::text
	`, email, string(hash), displayName).Scan(&id)
	return id, err
}

// Login verifies credentials and returns a fresh access/refresh token pair.
func (s *Service) Login(ctx context.Context, email, password string) (string, string, error) {
	var id string
	var pwHash string
	err := s.pool.QueryRow(ctx, `
		select id::text, password_hash
		from users
		where email = $1 and is_active = true
	`, email).Scan(&id, &pwHash)
	if err != nil {
		return "", "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(pwHash), []byte(password)) != nil {
		return "", "", errors.New("invalid credentials")
	}

	accessToken, err := issueAccessTokenFn(s, id, email)
	if err != nil {
		return "", "", err
	}
	refreshToken, err := randomTokenFn(48)
	if err != nil {
		return "", "", err
	}
	_, err = s.pool.Exec(ctx, `
		insert into sessions(user_id, refresh_token_hash, expires_at)
		values ($1::uuid, $2, now() + interval '30 days')
	`, id, sha256Hex(refreshToken))
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

// Refresh rotates the session's refresh token and returns a new token pair.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	hash := sha256Hex(refreshToken)
	var userID string
	var email string
	var sessionID string
	err := s.pool.QueryRow(ctx, `
		select u.id::text, u.email, ss.id::text
		from sessions ss
		join users u on u.id = ss.user_id
		where ss.refresh_token_hash = $1 and ss.expires_at > now()
	`, hash).Scan(&userID, &email, &sessionID)
	if err != nil {
		return "", "", err
	}

	accessToken, err := issueAccessTokenFn(s, userID, email)
	if err != nil {
		return "", "", err
	}
	nextRefresh, err := randomTokenFn(48)
	if err != nil {
		return "", "", err
	}
	_, err = s.pool.Exec(ctx, `
		update sessions
		set refresh_token_hash = $2, expires_at = now() + interval '30 days'
		where id = $1::uuid
	`, sessionID, sha256Hex(nextRefresh))
	if err != nil {
		return "", "", err
	}
	return accessToken, nextRefresh, nil
}

// Logout deletes the session identified by the given refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where refresh_token_hash = $1`, sha256Hex(refreshToken))
	return err
}

// ParseAccessToken validates a signed access token and returns its claims.
func (s *Service) ParseAccessToken(tokenRaw string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenRaw, &Claims{}, func(token *jwt.Token) (any, error) {
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *Service) issueAccessToken(userID, email string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
