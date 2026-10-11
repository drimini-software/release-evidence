package review

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
)

func TestExportFormatsPreserveCurrentReviewWithoutUpdatingSavedState(t *testing.T) {
	for _, format := range []struct {
		name, contentType, filename string
	}{
		{"html", "text/html; charset=utf-8", "release-evidence-report.html"},
		{"markdown", "text/markdown; charset=utf-8", "release-evidence-report.md"},
		{"json", "application/json; charset=utf-8", "release-evidence-packet.json"},
	} {
		t.Run(format.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "packet.json")
			session := testSession(t, statePath)
			if err := packet.WriteAtomic(statePath, session.packet, true); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			session.packet.Assessment = model.Assessment{
				Status: "in_progress", Context: "Current review context",
				Items: []model.AssessmentItem{{FindingID: session.packet.Findings[0].ID, Disposition: "assigned", Owner: "Local owner", Rationale: "Residual action"}},
			}
			session.packet.Decision = model.Decision{Status: "needs_work", Rationale: "Review remains open"}
			current := session.packet
			response := httptest.NewRecorder()
			session.ServeHTTP(response, exportRequest(`{"format":"`+format.name+`"}`))
			if response.Code != http.StatusOK {
				t.Fatalf("export status = %d: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != format.contentType || response.Header().Get("Content-Disposition") != `attachment; filename="`+format.filename+`"` {
				t.Fatalf("unexpected download headers: %v", response.Header())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("packet downloads must not be cached")
			}
			for _, text := range []string{"Current review context", "Local owner", "Residual action", "Review remains open", current.Integrity.Digest, current.Findings[0].ID} {
				if !strings.Contains(response.Body.String(), text) {
					t.Fatalf("download omitted %q", text)
				}
			}
			if format.name == "json" {
				downloaded, err := packet.Decode(bytes.NewReader(response.Body.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(downloaded, current) {
					t.Fatal("JSON download changed the current packet")
				}
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !reflect.DeepEqual(current, session.packet) {
				t.Fatal("export modified saved state or the session packet")
			}
		})
	}
}

func TestExportDoesNotCreateReviewState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	response := httptest.NewRecorder()
	session.ServeHTTP(response, exportRequest(`{"format":"json"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("export created review state: %v", err)
	}
}

func TestExportRejectsUnauthorizedAndInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		body   string
		status int
	}{
		{"wrong host", func(r *http.Request) { r.Host = "example.com" }, "", http.StatusForbidden},
		{"missing origin", func(r *http.Request) { r.Header.Del("Origin") }, "", http.StatusForbidden},
		{"cross origin", func(r *http.Request) { r.Header.Set("Origin", "https://example.com") }, "", http.StatusForbidden},
		{"missing token", func(r *http.Request) { r.Header.Del("X-Release-Evidence-Token") }, "", http.StatusUnauthorized},
		{"invalid token", func(r *http.Request) { r.Header.Set("X-Release-Evidence-Token", "fake-session-token") }, "", http.StatusUnauthorized},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodGet }, "", http.StatusMethodNotAllowed},
		{"wrong content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, "", http.StatusUnsupportedMediaType},
		{"invalid JSON", nil, "{", http.StatusBadRequest},
		{"unknown fields", nil, `{"format":"html","path":"packet.json"}`, http.StatusBadRequest},
		{"trailing JSON", nil, `{"format":"html"}{}`, http.StatusBadRequest},
		{"unsupported format", nil, `{"format":"pdf"}`, http.StatusBadRequest},
		{"missing format", nil, `{}`, http.StatusBadRequest},
		{"null request", nil, `null`, http.StatusBadRequest},
		{"oversized request", nil, `{"format":"` + strings.Repeat("x", maxExportRequestBytes) + `"}`, http.StatusBadRequest},
		{"oversized trailing whitespace", nil, `{"format":"html"}` + strings.Repeat(" ", maxExportRequestBytes), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := testSession(t, "")
			body := test.body
			if body == "" {
				body = `{"format":"html"}`
			}
			request := exportRequest(body)
			if test.change != nil {
				test.change(request)
			}
			response := httptest.NewRecorder()
			session.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if response.Header().Get("Content-Disposition") != "" {
				t.Fatal("rejected request returned download headers")
			}
		})
	}
}

func TestExportRefusesInvalidPacketsBeforeDownloadHeaders(t *testing.T) {
	for _, format := range []string{"html", "markdown", "json"} {
		for _, invalid := range []string{"schema", "digest"} {
			t.Run(format+"/"+invalid, func(t *testing.T) {
				session := testSession(t, "")
				if invalid == "schema" {
					session.packet.SchemaVersion = "unsupported"
				} else {
					session.packet.Findings[0].Explanation += " Altered after scanning."
				}
				response := httptest.NewRecorder()
				session.ServeHTTP(response, exportRequest(`{"format":"`+format+`"}`))
				if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Disposition") != "" {
					t.Fatalf("invalid packet was offered as a download: %d %v", response.Code, response.Header())
				}
				if !strings.Contains(response.Body.String(), "No download was created") {
					t.Fatal("export failure did not explain that no download was created")
				}
			})
		}
	}
}

func exportRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, testOrigin+"/api/export", strings.NewReader(body))
	request.Host = testHost
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("X-Release-Evidence-Token", testToken)
	request.Header.Set("Content-Type", "application/json")
	return request
}
