package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lholliger/hive/internal/store"
)

func TestStoreMiddleware(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	s := store.NewFromDB(db)

	var capturedStore *store.Store
	handler := StoreMiddleware(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedStore = GetStore(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, s, capturedStore)
}

func TestGetStoreFromEmptyContext(t *testing.T) {
	s := GetStore(context.Background())
	assert.Nil(t, s)
}
