package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrSessionExpired     = errors.New("session expired")
	ErrSessionNotFound    = errors.New("session not found")
)

const (
	CookieName     = "hive_session"
	sessionExpiry  = 30 * 24 * time.Hour // 30 days
	bcryptCost     = 12
	tokenBytes     = 32
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID        string    `json:"id"`
	Token     string    `json:"-"`
	UserID    string    `json:"user_id"`
	ActiveOrg string    `json:"active_org"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	db  *sql.DB
	log *zap.SugaredLogger
}

func NewService(db *sql.DB, log *zap.SugaredLogger) *Service {
	return &Service{db: db, log: log}
}

func (s *Service) Register(ctx context.Context, name, email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}

	user := &User{}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO auth_user (email, name, password_hash) VALUES ($1, $2, $3)
		 RETURNING id, email, name, created_at, updated_at`,
		email, name, string(hash),
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*Session, *User, error) {
	user := &User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, created_at, updated_at FROM auth_user WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	session, err := s.createSession(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return session, user, nil
}

func (s *Service) ValidateSession(ctx context.Context, token string) (*Session, *User, error) {
	sess := &Session{}
	user := &User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT s.id, s.token, s.user_id, s.active_org, s.expires_at, s.created_at,
		        u.id, u.email, u.name, u.created_at, u.updated_at
		 FROM auth_session s
		 JOIN auth_user u ON u.id = s.user_id
		 WHERE s.token = $1`, token,
	).Scan(
		&sess.ID, &sess.Token, &sess.UserID, &sess.ActiveOrg, &sess.ExpiresAt, &sess.CreatedAt,
		&user.ID, &user.Email, &user.Name, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrSessionNotFound
		}
		return nil, nil, err
	}

	if time.Now().After(sess.ExpiresAt) {
		_ = s.deleteSession(ctx, token)
		return nil, nil, ErrSessionExpired
	}

	return sess, user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.deleteSession(ctx, token)
}

func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	sess := &Session{}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO auth_session (token, user_id, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, token, user_id, active_org, expires_at, created_at`,
		token, userID, time.Now().Add(sessionExpiry),
	).Scan(&sess.ID, &sess.Token, &sess.UserID, &sess.ActiveOrg, &sess.ExpiresAt, &sess.CreatedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Service) deleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_session WHERE token = $1`, token)
	return err
}

func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "23505")
}
