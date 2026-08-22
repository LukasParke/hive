package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luke/hive/control-plane/internal/updater"
)

// fakeUpdater records CheckNow/Update calls and serves canned results.
type fakeUpdater struct {
	status      updater.Status
	checkErr    error
	updateErr   error
	checkCalls  int
	updateCalls int
}

func (f *fakeUpdater) Status() updater.Status { return f.status }

func (f *fakeUpdater) CheckNow(context.Context) error {
	f.checkCalls++
	return f.checkErr
}

func (f *fakeUpdater) Update(context.Context) error {
	f.updateCalls++
	return f.updateErr
}

func TestGetStatusEncodesUpdaterStatus(t *testing.T) {
	fake := &fakeUpdater{status: updater.Status{
		CurrentVersion:  "v1.2.3",
		LatestVersion:   "v1.3.0",
		UpdateAvailable: true,
		LastCheckedAt:   "2026-01-01T00:00:00Z",
	}}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/update/status", nil)
	rec := httptest.NewRecorder()
	h.GetStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got updater.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != fake.status {
		t.Fatalf("status = %+v, want %+v", got, fake.status)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content type = %s", rec.Header().Get("Content-Type"))
	}
}

func TestTriggerUpdateCheckFailureReturns500(t *testing.T) {
	fake := &fakeUpdater{checkErr: errors.New("github unreachable")}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/update/trigger", nil)
	rec := httptest.NewRecorder()
	h.TriggerUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "github unreachable") {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if fake.checkCalls != 1 || fake.updateCalls != 0 {
		t.Fatalf("calls = check:%d update:%d", fake.checkCalls, fake.updateCalls)
	}
}

func TestTriggerUpdateNoUpdateAvailable(t *testing.T) {
	fake := &fakeUpdater{status: updater.Status{CurrentVersion: "v2.0.0", LatestVersion: "v2.0.0"}}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/update/trigger", nil)
	rec := httptest.NewRecorder()
	h.TriggerUpdate(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no update available") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.updateCalls != 0 {
		t.Fatalf("update calls = %d, want 0", fake.updateCalls)
	}
	var resp struct {
		Message string         `json:"message"`
		Status  updater.Status `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status.CurrentVersion != "v2.0.0" {
		t.Fatalf("embedded status = %+v", resp.Status)
	}
}

func TestTriggerUpdateApplyFailureReturns500(t *testing.T) {
	fake := &fakeUpdater{
		status:    updater.Status{CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0", UpdateAvailable: true},
		updateErr: errors.New("swarm rolling update failed"),
	}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/update/trigger", nil)
	rec := httptest.NewRecorder()
	h.TriggerUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "rolling update failed") {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if fake.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", fake.updateCalls)
	}
}

func TestTriggerUpdateSuccess(t *testing.T) {
	fake := &fakeUpdater{status: updater.Status{
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
	}}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/update/trigger", nil)
	rec := httptest.NewRecorder()
	h.TriggerUpdate(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "update triggered") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "v1.1.0" {
		t.Fatalf("version = %q, want v1.1.0", resp.Version)
	}
	if fake.checkCalls != 1 || fake.updateCalls != 1 {
		t.Fatalf("calls = check:%d update:%d", fake.checkCalls, fake.updateCalls)
	}
}
