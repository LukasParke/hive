package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"

	"go.uber.org/zap"
)

func NewMockStore() (*store.Store, sqlmock.Sqlmock, error) {
	db, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, err
	}
	s := store.NewFromDB(db)
	return s, mock, nil
}

func TestLogger() *zap.SugaredLogger {
	l, _ := zap.NewNop().Sugar(), error(nil)
	return l
}

func RequestWithChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func RequestWithStore(r *http.Request, s *store.Store) *http.Request {
	mw := middleware.StoreMiddleware(s)
	rr := httptest.NewRecorder()
	var captured *http.Request
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
	})).ServeHTTP(rr, r)
	return captured
}

var TestUser = middleware.SessionUser{
	ID:    "user-1",
	Email: "test@test.com",
	Name:  "Test User",
}

func RequestWithSession(r *http.Request, user *middleware.SessionUser, orgID string) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.UserContextKey, user)
	session := &middleware.SessionData{
		User: *user,
	}
	session.Session.OrgID = orgID
	ctx = context.WithValue(ctx, middleware.SessionContextKey, session)
	return r.WithContext(ctx)
}
