package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/drimini-software/release-evidence/internal/comparison"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/report"
)

const maxComparisonPacketBytes = 4 << 20

// comparePacket accepts a saved packet as JSON, never a local filesystem path.
// The earlier packet is used only for this response and is not retained.
func (session *session) comparePacket(writer http.ResponseWriter, request *http.Request) {
	if !session.authorizeDownload(writer, request) {
		return
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["format"]) != 1 {
		writeError(writer, http.StatusBadRequest, "Choose one comparison format: html, markdown, or json.")
		return
	}
	format := query.Get("format")
	var contentType, filename string
	switch format {
	case "html":
		contentType, filename = "text/html; charset=utf-8", "release-evidence-comparison.html"
	case "markdown":
		contentType, filename = "text/markdown; charset=utf-8", "release-evidence-comparison.md"
	case "json":
		contentType, filename = "application/json; charset=utf-8", "release-evidence-comparison.json"
	default:
		writeError(writer, http.StatusBadRequest, "Comparison format must be html, markdown, or json.")
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxComparisonPacketBytes)
	previous, err := packet.Decode(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(writer, http.StatusRequestEntityTooLarge, "The earlier packet exceeds the 4 MiB limit.")
		} else {
			writeError(writer, http.StatusBadRequest, "The earlier packet is invalid, unsupported, or has a mismatched evidence digest. Select a saved Release Evidence JSON packet.")
		}
		return
	}

	session.mu.RLock()
	current := session.packet
	session.mu.RUnlock()
	var output bytes.Buffer
	if format == "json" {
		var result comparison.Result
		result, err = comparison.Compare(previous, current)
		if err == nil {
			encoder := json.NewEncoder(&output)
			encoder.SetIndent("", "  ")
			err = encoder.Encode(result)
		}
	} else {
		err = report.Render(&output, current, &previous, format)
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "The current packet could not be compared. No download was created.")
		return
	}

	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(output.Bytes())
}
