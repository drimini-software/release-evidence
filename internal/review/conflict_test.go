package review

import (
	"encoding/json"
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

func readReviewVersion(t *testing.T, session *session) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/packet", nil)
	request.Host = testHost
	request.Header.Set("X-Release-Evidence-Token", testToken)
	response := httptest.NewRecorder()
	session.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal("could not read the review packet")
	}
	return response.Header().Get("ETag")
}

func reviewSaveRequest(context, version string) *http.Request {
	body, _ := json.Marshal(reviewUpdate{
		Assessment: model.Assessment{Status: "in_progress", Context: context},
		Decision:   model.Decision{Status: "not_made"},
	})
	request := httptest.NewRequest(http.MethodPut, testOrigin+"/api/review", strings.NewReader(string(body)))
	request.Host = testHost
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Release-Evidence-Token", testToken)
	if version != "" {
		request.Header.Set("If-Match", version)
	}
	return request
}

func TestStaleReviewCannotOverwriteNewerSavedJudgment(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	loadedVersion := readReviewVersion(t, session)
	if loadedVersion == "" {
		t.Fatal("packet response is missing its review version")
	}
	first := httptest.NewRecorder()
	session.ServeHTTP(first, reviewSaveRequest("Newer review from the first tab", loadedVersion))
	if first.Code != http.StatusOK {
		t.Fatalf("first save failed: %d %s", first.Code, first.Body.String())
	}
	storedBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRecorder()
	session.ServeHTTP(stale, reviewSaveRequest("Stale review from the second tab", loadedVersion))
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale tab overwrote the newer review: status %d, want 412", stale.Code)
	}
	storedAfter, err := os.ReadFile(statePath)
	if err != nil || string(storedBefore) != string(storedAfter) {
		t.Fatal("stale save changed the saved packet")
	}
	stored, err := packet.Load(statePath)
	if err != nil || stored.Assessment.Context != "Newer review from the first tab" || session.packet.Assessment.Context != stored.Assessment.Context {
		t.Fatal("the newer judgment was lost from disk or the current session")
	}
}

func TestReviewVersionsAdvanceOnlyAfterSuccessfulSave(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	version := readReviewVersion(t, session)
	if version == "" || strings.HasPrefix(version, "W/") || readReviewVersion(t, session) != version {
		t.Fatal("unchanged packet reads must return a stable strong version")
	}
	invalid := reviewSaveRequest("Invalid update", version)
	invalid.Body = http.NoBody
	response := httptest.NewRecorder()
	session.ServeHTTP(response, invalid)
	if response.Code != http.StatusBadRequest || readReviewVersion(t, session) != version {
		t.Fatal("invalid update advanced the review version")
	}
	for _, context := range []string{"First edit", "Second edit"} {
		response := httptest.NewRecorder()
		session.ServeHTTP(response, reviewSaveRequest(context, version))
		next := response.Header().Get("ETag")
		if response.Code != http.StatusOK || next == "" || next == version || readReviewVersion(t, session) != next {
			t.Fatalf("successful save did not return its new version: %d %v", response.Code, response.Header())
		}
		version = next
	}
}

func TestReviewRequiresOneExactVersionBeforeChangingState(t *testing.T) {
	for _, name := range []string{"missing", "wrong", "wildcard", "weak", "list", "duplicate header", "other session"} {
		t.Run(name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "packet.json")
			session := testSession(t, statePath)
			version := readReviewVersion(t, session)
			current := session.packet
			request := reviewSaveRequest("private-unsaved-context", version)
			status := http.StatusPreconditionFailed
			switch name {
			case "missing":
				request.Header.Del("If-Match")
				status = http.StatusPreconditionRequired
			case "wrong":
				request.Header.Set("If-Match", `"wrong-review"`)
			case "wildcard":
				request.Header.Set("If-Match", "*")
			case "weak":
				request.Header.Set("If-Match", "W/"+version)
			case "list":
				request.Header.Set("If-Match", version+", "+version)
			case "duplicate header":
				request.Header.Add("If-Match", version)
			case "other session":
				request.Header.Set("If-Match", readReviewVersion(t, testSession(t, "")))
			}
			response := httptest.NewRecorder()
			session.ServeHTTP(response, request)
			if response.Code != status || response.Header().Get("ETag") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("invalid version produced %d %v", response.Code, response.Header())
			}
			if !reflect.DeepEqual(current, session.packet) || readReviewVersion(t, session) != version {
				t.Fatal("invalid version modified the current review")
			}
			if _, err := os.Stat(statePath); !os.IsNotExist(err) {
				t.Fatal("invalid version created saved state")
			}
			if strings.Contains(response.Body.String(), "private-unsaved-context") || strings.Contains(response.Body.String(), testToken) {
				t.Fatal("precondition error disclosed supplied text or the capability token")
			}
		})
	}
}

func TestFailedPersistenceKeepsVersionAndAllowsRetry(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, statePath)
	version := readReviewVersion(t, session)
	current := session.packet
	response := httptest.NewRecorder()
	session.ServeHTTP(response, reviewSaveRequest("Retry after disk failure", version))
	if response.Code != http.StatusInternalServerError || readReviewVersion(t, session) != version || !reflect.DeepEqual(current, session.packet) {
		t.Fatal("failed save changed the packet or its version")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	retry := httptest.NewRecorder()
	session.ServeHTTP(retry, reviewSaveRequest("Retry after disk failure", version))
	if retry.Code != http.StatusOK || retry.Header().Get("ETag") == version {
		t.Fatal("retry with the unchanged version did not succeed")
	}
}

func TestCurrentReviewVersionDoesNotBypassRequestAuthorization(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"wrong host", func(r *http.Request) { r.Host = "example.invalid" }, http.StatusForbidden},
		{"missing origin", func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden},
		{"cross origin", func(r *http.Request) { r.Header.Set("Origin", "https://example.invalid") }, http.StatusForbidden},
		{"missing token", func(r *http.Request) { r.Header.Del("X-Release-Evidence-Token") }, http.StatusUnauthorized},
		{"version as token", func(r *http.Request) { r.Header.Set("X-Release-Evidence-Token", r.Header.Get("If-Match")) }, http.StatusUnauthorized},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodPost }, http.StatusMethodNotAllowed},
		{"wrong type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := testSession(t, "")
			version := readReviewVersion(t, session)
			current := session.packet
			request := reviewSaveRequest("Unauthorized edit", version)
			test.change(request)
			response := httptest.NewRecorder()
			session.ServeHTTP(response, request)
			if response.Code != test.status || !reflect.DeepEqual(current, session.packet) || readReviewVersion(t, session) != version {
				t.Fatalf("current version bypassed request controls: status %d", response.Code)
			}
		})
	}
}

func TestConcurrentSavesFromOneVersionCommitOnlyOneJudgment(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "packet.json")
	session := testSession(t, statePath)
	version := readReviewVersion(t, session)
	start := make(chan struct{})
	type outcome struct {
		context string
		status  int
	}
	results := make(chan outcome, 2)
	for _, context := range []string{"Concurrent edit A", "Concurrent edit B"} {
		request := reviewSaveRequest(context, version)
		go func(context string, request *http.Request) {
			<-start
			response := httptest.NewRecorder()
			session.ServeHTTP(response, request)
			results <- outcome{context, response.Code}
		}(context, request)
	}
	close(start)
	winner := ""
	statuses := make(map[int]int)
	for index := 0; index < 2; index++ {
		result := <-results
		statuses[result.status]++
		if result.status == http.StatusOK {
			winner = result.context
		}
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusPreconditionFailed] != 1 {
		t.Fatalf("concurrent writers did not produce one winner and one conflict: %v", statuses)
	}
	stored, err := packet.Load(statePath)
	if err != nil || stored.Assessment.Context != winner || session.packet.Assessment.Context != winner {
		t.Fatal("disk and session do not contain the winning judgment")
	}
}
