package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/testdb"
)

const testPassword = "sup3rsecret!"

func uniqueEmail(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12] + "@test.local"
}

// sha256HexForTest mirrors the service's refresh-token hashing.
func sha256HexForTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func mustRegister(t *testing.T, email string) string {
	t.Helper()
	userID, err := testdb.Auth(t).Register(context.Background(), email, testPassword, "Test User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return userID
}

func TestRegisterCreatesUserAndRejectsDuplicates(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)

	email := uniqueEmail("dup")
	userID, err := svc.Register(context.Background(), email, testPassword, "Dup")
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	if userID == "" {
		t.Fatal("first register returned empty user id")
	}

	var storedEmail, hash, displayName string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select email, password_hash, display_name from users where id=$1::uuid`, userID,
	).Scan(&storedEmail, &hash, &displayName); err != nil {
		t.Fatalf("user row missing: %v", err)
	}
	if storedEmail != email || displayName != "Dup" {
		t.Fatalf("stored user = (%q, %q), want (%q, %q)", storedEmail, displayName, email, "Dup")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("password_hash %q is not bcrypt", hash)
	}

	// Duplicate email must surface the unique violation.
	if _, err := svc.Register(context.Background(), email, "another-pass", "Dup Again"); err == nil {
		t.Fatal("duplicate register err = nil, want error")
	} else if !strings.Contains(err.Error(), "duplicate key") && !strings.Contains(err.Error(), "unique") {
		t.Fatalf("duplicate register err = %v, want unique-violation error", err)
	}
}

func TestRegisterRejectsOverlongPassword(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)

	// bcrypt rejects passwords longer than 72 bytes before any DB work.
	_, err := testdb.Auth(t).Register(context.Background(),
		uniqueEmail("longpw"), strings.Repeat("x", 73), "Long PW")
	if err == nil {
		t.Fatal("overlong password err = nil, want bcrypt error")
	}
}

func TestLoginSuccessIssuesTokenPairAndSession(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)
	email := uniqueEmail("login")
	userID := mustRegister(t, email)

	access, refresh, err := svc.Login(context.Background(), email, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if access == "" || refresh == "" {
		t.Fatalf("login returned empty tokens: access=%q refresh=%q", access, refresh)
	}
	if access == refresh {
		t.Fatal("access and refresh tokens must differ")
	}

	claims, err := svc.ParseAccessToken(access)
	if err != nil {
		t.Fatalf("parse freshly issued access token: %v", err)
	}
	if claims.UserID != userID || claims.Email != email || claims.Subject != userID {
		t.Fatalf("claims identity = (%q, %q, sub=%q), want (%q, %q, sub=%q)",
			claims.UserID, claims.Email, claims.Subject, userID, email, userID)
	}
	if claims.ID == "" {
		t.Fatal("jti claim is empty")
	}
	now := time.Now()
	if !claims.IssuedAt.After(now.Add(-time.Minute)) {
		t.Fatalf("iat = %v, want ~now", claims.IssuedAt)
	}
	if !claims.ExpiresAt.After(now.Add(55*time.Minute)) || !claims.ExpiresAt.Before(now.Add(65*time.Minute)) {
		t.Fatalf("exp = %v, want ~1h from now", claims.ExpiresAt)
	}

	// Exactly one session row exists and its hash matches the issued refresh token.
	var sessions int
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select count(*) from sessions where user_id=$1::uuid`, userID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("session rows after login = %d, want 1", sessions)
	}
	var hash string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select refresh_token_hash from sessions where user_id=$1::uuid`, userID).Scan(&hash); err != nil {
		t.Fatalf("session row: %v", err)
	}
	wantHash := sha256HexForTest(refresh)
	if hash != wantHash {
		t.Fatalf("refresh_token_hash = %q, want sha256 of issued token %q", hash, wantHash)
	}
}

func TestLoginFailures(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)
	email := uniqueEmail("loginfail")

	cases := []struct {
		name    string
		setup   func(t *testing.T)
		loginAs string
		withPW  string
		wantErr string
	}{
		{
			name:    "unknown email",
			loginAs: uniqueEmail("ghost"),
			withPW:  testPassword,
			wantErr: pgx.ErrNoRows.Error(),
		},
		{
			name: "wrong password",
			setup: func(t *testing.T) {
				mustRegister(t, email)
			},
			loginAs: email,
			withPW:  "totally-wrong!",
			wantErr: "invalid credentials",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			access, refresh, err := svc.Login(context.Background(), tc.loginAs, tc.withPW)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Login err = %v, want %q", err, tc.wantErr)
			}
			if access != "" || refresh != "" {
				t.Fatalf("failed login returned tokens (%q, %q)", access, refresh)
			}
		})
	}
	t.Run("deactivated user cannot log in", func(t *testing.T) {
		inactive := uniqueEmail("inactive")
		mustRegister(t, inactive)
		if _, err := testdb.Get(t).Exec(context.Background(),
			`update users set is_active=false where email=$1`, inactive); err != nil {
			t.Fatalf("deactivate user: %v", err)
		}
		if _, _, err := svc.Login(context.Background(), inactive, testPassword); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("inactive login err = %v, want ErrNoRows", err)
		}
	})
}

func TestRefreshRotatesAndRejectsReuse(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)
	email := uniqueEmail("refresh")
	userID := mustRegister(t, email)

	oldAccess, oldRefresh, err := svc.Login(context.Background(), email, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	newAccess, newRefresh, err := svc.Refresh(context.Background(), oldRefresh)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newRefresh == "" || newRefresh == oldRefresh {
		t.Fatalf("rotated refresh token = %q, want a fresh distinct token", newRefresh)
	}
	if newAccess == "" || newAccess == oldAccess {
		t.Fatal("refresh must mint a new access token")
	}

	// The rotation updated the stored hash; the old token no longer resolves.
	if _, _, err := svc.Refresh(context.Background(), oldRefresh); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("reuse of rotated refresh err = %v, want ErrNoRows", err)
	}

	// The new token still maps to the same session/user.
	nextClaims, err := svc.ParseAccessToken(newAccess)
	if err != nil {
		t.Fatalf("parse refreshed access token: %v", err)
	}
	if nextClaims.UserID != userID || nextClaims.Email != email {
		t.Fatalf("refreshed claims = (%q, %q), want (%q, %q)",
			nextClaims.UserID, nextClaims.Email, userID, email)
	}
	secondAccess, secondRefresh, err := svc.Refresh(context.Background(), newRefresh)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if secondRefresh == "" || secondRefresh == newRefresh || secondAccess == "" {
		t.Fatal("second refresh did not rotate properly")
	}

	// Still only one session row for the user.
	var sessions int
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select count(*) from sessions where user_id=$1::uuid`, userID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("session rows after two refreshes = %d, want 1 (rotation, not creation)", sessions)
	}
}

func TestRefreshFailures(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)

	t.Run("unknown token", func(t *testing.T) {
		if _, _, err := svc.Refresh(context.Background(), "no-such-refresh-token"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("err = %v, want ErrNoRows", err)
		}
	})

	t.Run("expired session", func(t *testing.T) {
		email := uniqueEmail("expired")
		mustRegister(t, email)
		_, refresh, err := svc.Login(context.Background(), email, testPassword)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if _, err := testdb.Get(t).Exec(context.Background(),
			`update sessions set expires_at = now() - interval '1 hour' where refresh_token_hash = $1`,
			sha256HexForTest(refresh)); err != nil {
			t.Fatalf("expire session: %v", err)
		}
		if _, _, err := svc.Refresh(context.Background(), refresh); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expired refresh err = %v, want ErrNoRows", err)
		}
	})
}

func TestLogoutInvalidatesSession(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	svc := testdb.Auth(t)
	email := uniqueEmail("logout")
	mustRegister(t, email)

	_, refresh, err := svc.Login(context.Background(), email, testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := svc.Logout(context.Background(), refresh); err != nil {
		t.Fatalf("logout: %v", err)
	}
	// The deleted session can no longer be refreshed.
	if _, _, err := svc.Refresh(context.Background(), refresh); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("refresh after logout err = %v, want ErrNoRows", err)
	}
	if n := testdb.QueryCount(t, `select count(*) from sessions where refresh_token_hash = $1`, sha256HexForTest(refresh)); n != 0 {
		t.Fatalf("session rows after logout = %d, want 0", n)
	}
	if err := svc.Logout(context.Background(), refresh); err != nil {
		t.Fatalf("logout unknown token err = %v, want nil", err)
	}
}

func TestParseAccessTokenRejectsBadTokens(t *testing.T) {
	svc := auth.NewService(nil, "test-jwt-secret")

	t.Run("garbage input", func(t *testing.T) {
		if _, err := svc.ParseAccessToken("not.a.jwt"); err == nil {
			t.Fatal("garbage token err = nil, want error")
		}
	})

	t.Run("wrong signing secret", func(t *testing.T) {
		other := auth.NewService(nil, "some-other-secret")
		token, err := mintWithSecret("some-other-secret")
		if err != nil {
			t.Fatalf("mint foreign token: %v", err)
		}
		_ = other
		if _, err := svc.ParseAccessToken(token); err == nil {
			t.Fatal("foreign-secret token err = nil, want error")
		}
	})

	t.Run("disallowed signing method", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS512, auth.Claims{
			UserID: "u1",
			Email:  "u1@test.local",
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "u1",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		})
		raw, err := token.SignedString([]byte("test-jwt-secret"))
		if err != nil {
			t.Fatalf("sign HS512: %v", err)
		}
		if _, err := svc.ParseAccessToken(raw); err == nil {
			t.Fatal("HS512 token err = nil, want error")
		} else if !strings.Contains(err.Error(), "signing method") {
			t.Fatalf("HS512 err = %v, want signing-method rejection", err)
		}
	})
}

// mintWithSecret signs a minimal HS256 claims set with an arbitrary secret so
// signature-mismatch paths can be exercised.
func mintWithSecret(secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":   "user-1",
		"email": "user-1@test.local",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	return token.SignedString([]byte(secret))
}
