package review

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
)

const (
	maxReviewBytes   = 256 << 10
	defaultIdleLimit = 30 * time.Minute
)

//go:embed assets/index.html assets/styles.css assets/app.js
var embeddedAssets embed.FS

type Options struct {
	ListenAddress string
	StatePath     string
	Writer        io.Writer
	IdleTimeout   time.Duration
	Now           func() time.Time
}

type reviewUpdate struct {
	Assessment model.Assessment `json:"assessment"`
	Decision   model.Decision   `json:"decision"`
}

type session struct {
	mu             sync.RWMutex
	packet         model.Packet
	token          string
	expectedHost   string
	expectedOrigin string
	statePath      string
	now            func() time.Time
	page           *template.Template
	onActivity     func()
}

func Serve(ctx context.Context, current model.Packet, options Options) error {
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = defaultIdleLimit
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.ListenAddress == "" {
		options.ListenAddress = "127.0.0.1:0"
	}
	if err := validateListenAddress(options.ListenAddress); err != nil {
		return err
	}

	listener, err := net.Listen("tcp4", options.ListenAddress)
	if err != nil {
		return fmt.Errorf("start loopback review listener: %w", err)
	}
	defer listener.Close()

	token, err := newToken()
	if err != nil {
		return err
	}
	expectedHost := listener.Addr().String()
	origin := "http://" + expectedHost

	activity := make(chan struct{}, 1)
	reviewContext, cancel := context.WithCancel(ctx)
	defer cancel()

	session, err := newSession(current, token, expectedHost, origin, options.StatePath, options.Now, func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:           session,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go stopAfterIdle(reviewContext, cancel, activity, options.IdleTimeout)
	go func() {
		<-reviewContext.Done()
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownContext)
	}()

	fmt.Fprintf(options.Writer, "Review locally at %s\n", origin)
	fmt.Fprintf(options.Writer, "Review state: %s\n", options.StatePath)
	fmt.Fprintf(options.Writer, "The session stops after %s of inactivity or when interrupted.\n", options.IdleTimeout)

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve local review: %w", err)
	}
	return nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse review listen address: %w", err)
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("review listen address must use the IPv4 loopback host 127.0.0.1")
	}
	if port == "" {
		return fmt.Errorf("review listen address must include a port or port 0")
	}
	return nil
}

func RestoreAssessment(current model.Packet, statePath string) (model.Packet, error) {
	stored, err := packet.Load(statePath)
	if os.IsNotExist(err) {
		return current, nil
	}
	if err != nil {
		return model.Packet{}, fmt.Errorf("load prior review state: %w", err)
	}
	if stored.Repository.Name != current.Repository.Name {
		return model.Packet{}, fmt.Errorf("stored review belongs to a different repository")
	}

	validFindingIDs := make(map[string]struct{}, len(current.Findings))
	for _, finding := range current.Findings {
		validFindingIDs[finding.ID] = struct{}{}
	}
	items := make([]model.AssessmentItem, 0, len(stored.Assessment.Items))
	for _, item := range stored.Assessment.Items {
		if _, exists := validFindingIDs[item.FindingID]; exists {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].FindingID < items[j].FindingID })

	current.Assessment = stored.Assessment
	current.Assessment.Items = items
	current.Decision = stored.Decision
	if err := packet.SetIntegrity(&current); err != nil {
		return model.Packet{}, err
	}
	if err := model.ValidatePacket(current); err != nil {
		return model.Packet{}, fmt.Errorf("merge prior review state: %w", err)
	}
	return current, nil
}

func DefaultStatePath(repositoryRoot string) (string, error) {
	absolute, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository for state path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve repository links for state path: %w", err)
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(resolved))))
	projectKey := hex.EncodeToString(digest[:8])
	return filepath.Join(configRoot, "Drimini", "Release Evidence", "projects", projectKey, "packet.json"), nil
}

func newSession(current model.Packet, token, expectedHost, expectedOrigin, statePath string, now func() time.Time, onActivity func()) (*session, error) {
	page, err := template.ParseFS(embeddedAssets, "assets/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse review interface: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	if onActivity == nil {
		onActivity = func() {}
	}
	return &session{
		packet:         current,
		token:          token,
		expectedHost:   expectedHost,
		expectedOrigin: expectedOrigin,
		statePath:      statePath,
		now:            now,
		page:           page,
		onActivity:     onActivity,
	}, nil
}

func (session *session) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(writer)
	if request.Host != session.expectedHost {
		writeError(writer, http.StatusForbidden, "The local review host is not allowed.")
		return
	}
	if origin := request.Header.Get("Origin"); origin != "" && origin != session.expectedOrigin {
		writeError(writer, http.StatusForbidden, "Cross-origin review requests are not allowed.")
		return
	}
	session.onActivity()

	switch request.URL.Path {
	case "/":
		session.servePage(writer, request)
	case "/assets/app.js":
		serveEmbedded(writer, request, "assets/app.js", "text/javascript; charset=utf-8")
	case "/assets/styles.css":
		serveEmbedded(writer, request, "assets/styles.css", "text/css; charset=utf-8")
	case "/api/packet":
		session.servePacket(writer, request)
	case "/api/review":
		session.updateReview(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "Not found.")
	}
}

func (session *session) servePage(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := session.page.ExecuteTemplate(writer, "index.html", struct{ Token string }{Token: session.token}); err != nil {
		return
	}
}

func serveEmbedded(writer http.ResponseWriter, request *http.Request, name, contentType string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	content, err := embeddedAssets.ReadFile(name)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Interface asset unavailable.")
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content)
}

func (session *session) servePacket(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if !session.authorized(request) {
		writeError(writer, http.StatusUnauthorized, "The review session token is missing or invalid.")
		return
	}
	session.mu.RLock()
	current := session.packet
	session.mu.RUnlock()
	writeJSON(writer, http.StatusOK, current)
}

func (session *session) updateReview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		methodNotAllowed(writer, http.MethodPut)
		return
	}
	if request.Header.Get("Origin") != session.expectedOrigin {
		writeError(writer, http.StatusForbidden, "A same-origin review request is required.")
		return
	}
	if !session.authorized(request) {
		writeError(writer, http.StatusUnauthorized, "The review session token is missing or invalid.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "Review updates must use application/json.")
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxReviewBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var update reviewUpdate
	if err := decoder.Decode(&update); err != nil {
		writeError(writer, http.StatusBadRequest, "The review update is invalid or too large.")
		return
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "The review update contains trailing JSON content.")
		return
	}

	now := session.now().UTC().Format(time.RFC3339Nano)
	update.Assessment.UpdatedAt = now
	update.Decision.UpdatedAt = now
	sort.Slice(update.Assessment.Items, func(i, j int) bool {
		return update.Assessment.Items[i].FindingID < update.Assessment.Items[j].FindingID
	})

	session.mu.Lock()
	defer session.mu.Unlock()
	candidate := session.packet
	candidate.Assessment = update.Assessment
	candidate.Decision = update.Decision
	if err := packet.SetIntegrity(&candidate); err != nil {
		writeError(writer, http.StatusInternalServerError, "The local packet could not be updated.")
		return
	}
	if err := model.ValidatePacket(candidate); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if session.statePath != "" {
		if err := packet.WriteAtomic(session.statePath, candidate, true); err != nil {
			writeError(writer, http.StatusInternalServerError, "The local packet could not be saved.")
			return
		}
	}
	session.packet = candidate
	writeJSON(writer, http.StatusOK, candidate)
}

func (session *session) authorized(request *http.Request) bool {
	provided := request.Header.Get("X-Release-Evidence-Token")
	if len(provided) != len(session.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(session.token)) == 1
}

func newToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create review session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func stopAfterIdle(ctx context.Context, cancel context.CancelFunc, activity <-chan struct{}, idle time.Duration) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			cancel()
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
		}
	}
}

func setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; script-src 'self'; style-src 'self'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeError(writer, http.StatusMethodNotAllowed, "Method not allowed.")
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(writer http.ResponseWriter, status int, value interface{}) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
