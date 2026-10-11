package review

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/report"
)

const maxExportRequestBytes = 1024

// exportPacket renders one in-memory snapshot. It never scans the repository,
// writes review state, or accepts a server-side output path.
func (session *session) exportPacket(writer http.ResponseWriter, request *http.Request) {
	if !session.authorizeDownload(writer, request) {
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxExportRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var options struct {
		Format string `json:"format"`
	}
	if err := decoder.Decode(&options); err != nil {
		writeError(writer, http.StatusBadRequest, "The export request is invalid or too large.")
		return
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "The export request contains trailing JSON content.")
		return
	}

	var contentType, filename string
	switch options.Format {
	case "html":
		contentType, filename = "text/html; charset=utf-8", "release-evidence-report.html"
	case "markdown":
		contentType, filename = "text/markdown; charset=utf-8", "release-evidence-report.md"
	case "json":
		contentType, filename = "application/json; charset=utf-8", "release-evidence-packet.json"
	default:
		writeError(writer, http.StatusBadRequest, "Export format must be html, markdown, or json.")
		return
	}

	// Updates replace the packet and its human sections rather than mutating
	// their slices. This snapshot stays stable after releasing the read lock.
	session.mu.RLock()
	current := session.packet
	session.mu.RUnlock()

	var output bytes.Buffer
	var err error
	if options.Format == "json" {
		err = model.ValidatePacket(current)
		if err == nil {
			err = packet.VerifyIntegrity(current)
		}
		if err == nil {
			err = packet.Encode(&output, current, true)
		}
	} else {
		err = report.Render(&output, current, nil, options.Format)
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "The current packet could not be exported. No download was created.")
		return
	}

	// Render completely before committing attachment headers. Names are fixed
	// constants so repository or reviewer text cannot enter response headers.
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(output.Bytes())
}

func (session *session) authorizeDownload(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return false
	}
	if request.Header.Get("Origin") != session.expectedOrigin {
		writeError(writer, http.StatusForbidden, "A same-origin review request is required.")
		return false
	}
	if !session.authorized(request) {
		writeError(writer, http.StatusUnauthorized, "The review session token is missing or invalid.")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "Download requests must use application/json.")
		return false
	}
	return true
}
