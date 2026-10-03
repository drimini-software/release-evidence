package review

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/repository"
	"github.com/drimini-software/release-evidence/internal/scanner"
)

const (
	testToken  = "test-session-token"
	testHost   = "127.0.0.1:43117"
	testOrigin = "http://127.0.0.1:43117"
)

func TestPageHasSecurityHeadersAndSessionToken(t *testing.T) {
	session := testSession(t, "")
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/", nil)
	request.Host = testHost
	response := httptest.NewRecorder()
	session.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), testToken) {
		t.Fatal("page does not contain the session bootstrap token")
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("security or no-store headers are missing")
	}
}

func TestListenAddressMustRemainOnLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", "192.0.2.1:8080", "localhost:8080", ":8080"} {
		if err := validateListenAddress(address); err == nil {
			t.Fatalf("validateListenAddress(%q) accepted a non-explicit-loopback address", address)
		}
	}
	for _, address := range []string{"127.0.0.1:0", "127.0.0.1:43117"} {
		if err := validateListenAddress(address); err != nil {
			t.Fatalf("validateListenAddress(%q) error = %v", address, err)
		}
	}
}

func TestPacketAPIRequiresHostAndToken(t *testing.T) {
	session := testSession(t, "")

	wrongHost := httptest.NewRequest(http.MethodGet, testOrigin+"/api/packet", nil)
	wrongHost.Host = "example.com"
	wrongHost.Header.Set("X-Release-Evidence-Token", testToken)
	wrongHostResponse := httptest.NewRecorder()
	session.ServeHTTP(wrongHostResponse, wrongHost)
	if wrongHostResponse.Code != http.StatusForbidden {
		t.Fatalf("wrong-host status = %d, want 403", wrongHostResponse.Code)
	}

	missingToken := httptest.NewRequest(http.MethodGet, testOrigin+"/api/packet", nil)
	missingToken.Host = testHost
	missingTokenResponse := httptest.NewRecorder()
	session.ServeHTTP(missingTokenResponse, missingToken)
	if missingTokenResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing-token status = %d, want 401", missingTokenResponse.Code)
	}

	valid := httptest.NewRequest(http.MethodGet, testOrigin+"/api/packet", nil)
	valid.Host = testHost
	valid.Header.Set("X-Release-Evidence-Token", testToken)
	validResponse := httptest.NewRecorder()
	session.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid status = %d, want 200: %s", validResponse.Code, validResponse.Body.String())
	}
}

func TestReviewUpdateRequiresSameOriginAndPersistsAtomically(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	update := reviewUpdate{
		Assessment: model.Assessment{
			Status:  "in_progress",
			Context: "First production release",
			Items: []model.AssessmentItem{{
				FindingID:   "build.manifest",
				Disposition: "accepted",
				Rationale:   "The pinned manifest is part of this review.",
				Owner:       "Release owner",
			}},
		},
		Decision: model.Decision{
			Status:    "needs_work",
			Rationale: "CI evidence is still missing.",
			Owner:     "Release owner",
		},
	}
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	wrongOrigin := httptest.NewRequest(http.MethodPut, testOrigin+"/api/review", bytes.NewReader(body))
	wrongOrigin.Host = testHost
	wrongOrigin.Header.Set("Origin", "https://example.com")
	wrongOrigin.Header.Set("Content-Type", "application/json")
	wrongOrigin.Header.Set("X-Release-Evidence-Token", testToken)
	wrongOriginResponse := httptest.NewRecorder()
	session.ServeHTTP(wrongOriginResponse, wrongOrigin)
	if wrongOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin status = %d, want 403", wrongOriginResponse.Code)
	}

	valid := httptest.NewRequest(http.MethodPut, testOrigin+"/api/review", bytes.NewReader(body))
	valid.Host = testHost
	valid.Header.Set("Origin", testOrigin)
	valid.Header.Set("Content-Type", "application/json")
	valid.Header.Set("X-Release-Evidence-Token", testToken)
	validResponse := httptest.NewRecorder()
	session.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid update status = %d, want 200: %s", validResponse.Code, validResponse.Body.String())
	}

	stored, err := packet.Load(statePath)
	if err != nil {
		t.Fatalf("load persisted packet: %v", err)
	}
	if stored.Assessment.Status != "in_progress" || stored.Decision.Status != "needs_work" {
		t.Fatal("persisted review does not contain the submitted assessment and decision")
	}
	if stored.Assessment.UpdatedAt != "2026-09-27T12:00:00Z" || stored.Decision.UpdatedAt != "2026-09-27T12:00:00Z" {
		t.Fatal("server did not assign the fixed update time")
	}
}

func TestReviewUpdateRejectsUnknownFinding(t *testing.T) {
	session := testSession(t, "")
	update := `{"assessment":{"status":"in_progress","items":[{"finding_id":"missing.rule","disposition":"open"}]},"decision":{"status":"not_made"}}`
	request := httptest.NewRequest(http.MethodPut, testOrigin+"/api/review", strings.NewReader(update))
	request.Host = testHost
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Release-Evidence-Token", testToken)
	response := httptest.NewRecorder()
	session.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestRestoreAssessmentKeepsOnlyCurrentFindingIDs(t *testing.T) {
	current := testPacket(t)
	stored := current
	stored.Findings = append(stored.Findings, model.Finding{
		ID:             "zz.removed",
		RuleVersion:    "1",
		Category:       "fixture",
		Title:          "Removed rule",
		State:          model.StateNotObserved,
		Confidence:     model.ConfidenceMedium,
		Explanation:    "No evidence was observed.",
		Limitations:    "Migration fixture only.",
		SearchBoundary: []string{"fixture"},
		Citations:      []model.Citation{},
	})
	stored.Assessment = model.Assessment{
		Status: "in_progress",
		Items: []model.AssessmentItem{
			{FindingID: current.Findings[0].ID, Disposition: "resolved"},
			{FindingID: "zz.removed", Disposition: "open"},
		},
	}
	stored.Decision = model.Decision{Status: "needs_work"}
	if err := packet.SetIntegrity(&stored); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "packet.json")
	if err := packet.WriteAtomic(statePath, stored, true); err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreAssessment(current, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Assessment.Items) != 1 || restored.Assessment.Items[0].FindingID != current.Findings[0].ID {
		t.Fatal("restore did not filter assessment items to current finding IDs")
	}
}

func testSession(t *testing.T, statePath string) *session {
	t.Helper()
	result := testPacket(t)
	session, err := newSession(
		result,
		testToken,
		testHost,
		testOrigin,
		statePath,
		func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func testPacket(t *testing.T) model.Packet {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "complete"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), fixture, scanner.Options{
		Repository:  repository.DefaultOptions(),
		ToolVersion: "test",
		Now:         func() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
