package review

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/comparison"
	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
)

func TestComparisonDownloadsPreserveEvidenceAndSeparateHumanChanges(t *testing.T) {
	for _, format := range []struct{ name, contentType, filename string }{
		{"html", "text/html; charset=utf-8", "release-evidence-comparison.html"},
		{"markdown", "text/markdown; charset=utf-8", "release-evidence-comparison.md"},
		{"json", "application/json; charset=utf-8", "release-evidence-comparison.json"},
	} {
		t.Run(format.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "packet.json")
			session := testSession(t, statePath)
			if err := packet.WriteAtomic(statePath, session.packet, true); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := packet.Decode(bytes.NewReader(encodeReviewPacket(t, session.packet)))
			if err != nil {
				t.Fatal(err)
			}
			previous.Repository.Name = "Earlier repository"
			previous.Findings[0].Explanation = "Earlier observation"
			previous.Assessment = model.Assessment{Status: "in_progress", Context: "Earlier review context"}
			previous.Decision = model.Decision{Status: "blocked", Rationale: "Earlier decision rationale"}
			if err := packet.SetIntegrity(&previous); err != nil {
				t.Fatal(err)
			}
			session.packet.Assessment = model.Assessment{Status: "in_progress", Context: "Current review context"}
			session.packet.Decision = model.Decision{Status: "needs_work", Rationale: "Current decision rationale"}
			current := session.packet
			response := httptest.NewRecorder()
			session.ServeHTTP(response, comparisonRequest(encodeReviewPacket(t, previous), "format="+format.name))
			if response.Code != http.StatusOK {
				t.Fatalf("comparison status = %d: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != format.contentType || response.Header().Get("Content-Disposition") != `attachment; filename="`+format.filename+`"` {
				t.Fatalf("unexpected attachment headers: %v", response.Header())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("comparison downloads must not be cached")
			}
			for _, text := range []string{"Earlier observation", "Earlier review context", "Current review context", "Earlier decision rationale", "Current decision rationale", previous.Integrity.Digest, current.Integrity.Digest, "Repository names differ"} {
				if !strings.Contains(response.Body.String(), text) {
					t.Fatalf("comparison omitted %q", text)
				}
			}
			if format.name == "json" {
				var result comparison.Result
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Changes) != 1 || result.Changes[0].Kind != "changed" || result.Unchanged != len(current.Findings)-1 || !result.ContextChanged || !result.DecisionChanged {
					t.Fatal("evidence and human changes were lost or confused")
				}
				if !reflect.DeepEqual(result.AssessmentBefore, previous.Assessment) || !reflect.DeepEqual(result.AssessmentAfter, current.Assessment) {
					t.Fatal("comparison changed the human assessments")
				}
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, after) || !reflect.DeepEqual(current, session.packet) {
				t.Fatal("comparison changed review state or the current session packet")
			}
		})
	}
}

func TestComparisonDoesNotCreateReviewState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	response := httptest.NewRecorder()
	session.ServeHTTP(response, comparisonRequest(encodeReviewPacket(t, session.packet), "format=json"))
	if response.Code != http.StatusOK {
		t.Fatalf("comparison status = %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("comparison created saved review state: %v", err)
	}
}

func TestComparisonRejectsUnauthorizedRequestsAndInvalidFormats(t *testing.T) {
	for _, test := range []struct {
		name, query string
		change      func(*http.Request)
		status      int
	}{
		{"wrong host", "format=html", func(r *http.Request) { r.Host = "example.com" }, http.StatusForbidden},
		{"missing origin", "format=html", func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden},
		{"cross origin", "format=html", func(r *http.Request) { r.Header.Set("Origin", "https://example.com") }, http.StatusForbidden},
		{"missing token", "format=html", func(r *http.Request) { r.Header.Del("X-Release-Evidence-Token") }, http.StatusUnauthorized},
		{"invalid token", "format=html", func(r *http.Request) { r.Header.Set("X-Release-Evidence-Token", "fake-session-token") }, http.StatusUnauthorized},
		{"wrong method", "format=html", func(r *http.Request) { r.Method = http.MethodGet }, http.StatusMethodNotAllowed},
		{"wrong content type", "format=html", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType},
		{"missing format", "", nil, http.StatusBadRequest},
		{"invalid format", "format=pdf", nil, http.StatusBadRequest},
		{"duplicate format", "format=html&format=json", nil, http.StatusBadRequest},
		{"unknown parameter", "format=html&path=packet.json", nil, http.StatusBadRequest},
		{"malformed query", "format=%zz", nil, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := testSession(t, "")
			request := comparisonRequest(encodeReviewPacket(t, session.packet), test.query)
			if test.change != nil {
				test.change(request)
			}
			response := httptest.NewRecorder()
			session.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Content-Disposition") != "" {
				t.Fatalf("invalid request produced %d %v: %s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestComparisonRejectsInvalidEarlierPacketsWithoutDisclosure(t *testing.T) {
	session := testSession(t, "")
	valid := encodeReviewPacket(t, session.packet)
	for _, test := range []struct {
		name   string
		body   []byte
		status int
	}{
		{"malformed JSON", []byte("{"), http.StatusBadRequest},
		{"null", []byte("null"), http.StatusBadRequest},
		{"filesystem path", []byte(`{"path":"private-note-do-not-disclose"}`), http.StatusBadRequest},
		{"trailing JSON", append(append([]byte{}, valid...), []byte("{}")...), http.StatusBadRequest},
		{"unknown fields", bytes.Replace(valid, []byte("{\n"), []byte("{\n\"private-note-do-not-disclose\":true,"), 1), http.StatusBadRequest},
		{"duplicate decision", bytes.Replace(valid, []byte(`"status": "not_made"`), []byte(`"status": "approved", "status": "not_made"`), 1), http.StatusBadRequest},
		{"case alias", bytes.Replace(valid, []byte(`"status": "not_made"`), []byte(`"STATUS": "not_made"`), 1), http.StatusBadRequest},
		{"invalid UTF-8", bytes.Replace(valid, []byte(`"status": "not_made"`), []byte("\"status\": \"not_made\", \"rationale\": \"\xffprivate-note-do-not-disclose\""), 1), http.StatusBadRequest},
		{"invalid Unicode escape", bytes.Replace(valid, []byte(`"status": "not_made"`), []byte(`"status": "not_made", "rationale": "\ud800private-note-do-not-disclose"`), 1), http.StatusBadRequest},
		{"oversized packet", []byte(strings.Repeat(" ", maxComparisonPacketBytes+1)), http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			if bytes.Equal(test.body, valid) {
				t.Fatal("test did not mutate the earlier packet")
			}
			response := httptest.NewRecorder()
			session.ServeHTTP(response, comparisonRequest(test.body, "format=html"))
			if response.Code != test.status || response.Header().Get("Content-Disposition") != "" {
				t.Fatalf("invalid earlier packet produced %d %v", response.Code, response.Header())
			}
			if strings.Contains(response.Body.String(), "private-note-do-not-disclose") {
				t.Fatal("error response disclosed earlier packet content")
			}
		})
	}
	for _, invalid := range []string{"schema", "digest"} {
		previous := session.packet
		if invalid == "schema" {
			previous.SchemaVersion = "unsupported"
		} else {
			previous.Integrity.Digest = strings.Repeat("0", 64)
		}
		response := httptest.NewRecorder()
		session.ServeHTTP(response, comparisonRequest(encodeReviewPacket(t, previous), "format=json"))
		if response.Code != http.StatusBadRequest || response.Header().Get("Content-Disposition") != "" {
			t.Fatalf("earlier packet with invalid %s was accepted", invalid)
		}
	}
}

func TestComparisonRefusesInvalidCurrentPacketsBeforeAttachmentHeaders(t *testing.T) {
	for _, format := range []string{"html", "markdown", "json"} {
		t.Run(format, func(t *testing.T) {
			session := testSession(t, "")
			previous := encodeReviewPacket(t, session.packet)
			session.packet.Integrity.Digest = strings.Repeat("0", 64)
			response := httptest.NewRecorder()
			session.ServeHTTP(response, comparisonRequest(previous, "format="+format))
			if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Disposition") != "" {
				t.Fatalf("invalid current packet produced %d %v", response.Code, response.Header())
			}
			if !strings.Contains(response.Body.String(), "No download was created") {
				t.Fatal("comparison failure did not explain that no download was created")
			}
		})
	}
}

func comparisonRequest(body []byte, query string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, testOrigin+"/api/compare", bytes.NewReader(body))
	request.Host = testHost
	request.URL.RawQuery = query
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("X-Release-Evidence-Token", testToken)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func encodeReviewPacket(t *testing.T, current model.Packet) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := packet.Encode(&encoded, current, true); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
