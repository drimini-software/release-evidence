package packet_test

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/drimini-software/release-evidence/internal/comparison"
	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/report"
)

// Exercise the untrusted file/body boundary rather than JSON helpers alone.
// Every accepted mutation must retain its complete packet on re-encoding and
// work with the actual report and comparison consumers without a panic.
func FuzzDecodePacket(f *testing.F) {
	valid := encodeFixture(f, importFixture(f))
	for _, input := range [][]byte{
		valid, nil, []byte(`null`), []byte(`{}`), []byte(`{"schema_version":"999"}`),
		append(append([]byte{}, valid...), []byte(`{}`)...),
		bytes.Replace(valid, []byte(`"status":"not_made"`), []byte(`"status":"approved","status":"not_made"`), 1),
		bytes.Replace(valid, []byte(`"status":"not_made"`), []byte(`"STATUS":"not_made"`), 1),
		bytes.Replace(valid, []byte(`"status":"not_made"`), []byte("\"status\":\"not_made\",\"rationale\":\"\xff\""), 1),
		bytes.Replace(valid, []byte(`"status":"not_made"`), []byte(`"status":"not_made","rationale":"\ud800"`), 1),
		bytes.Replace(valid, []byte(`"status":"not_made"`), []byte(`"status":"not_made","rationale":"\ud83d\ude00"`), 1),
		[]byte(strings.Repeat(`[`, 100) + `0` + strings.Repeat(`]`, 100)),
	} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		p, err := packet.Decode(bytes.NewReader(input))
		if err != nil {
			if !reflect.DeepEqual(p, model.Packet{}) {
				t.Fatal("failed import returned a partial packet")
			}
			return
		}
		copy := roundTripPacket(t, p)
		for _, format := range []string{"html", "markdown"} {
			if err := report.Render(io.Discard, copy, &p, format); err != nil {
				t.Fatalf("accepted packet cannot render %s: %v", format, err)
			}
		}
	})
}

// Structured mutations keep the schema and digest valid so fuzzing reaches
// report escaping and human text instead of spending every case in rejection.
func FuzzPacketText(f *testing.F) {
	seed := importFixture(f)
	for _, text := range []string{
		"review note", "Unicode: café 日本語 😀", "` `` ```\r\n# heading\n![image](https://example.invalid/image)",
		`<script>alert(1)</script><img src="https://example.invalid/image" onerror="alert(1)">`,
		"quotes: \" \\ & < >\tline\u2028separator",
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if text == "" || len(text) > 1_000 || !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
			return
		}
		p := seed
		p.Findings = append([]model.Finding{}, seed.Findings...)
		p.Repository.Name = text
		p.Findings[0].Title, p.Findings[0].Explanation = text, text
		p.Assessment.Context, p.Decision.Rationale = text, text
		p.Assessment.Items = []model.AssessmentItem{{FindingID: p.Findings[0].ID, Disposition: "assigned", Rationale: text, Owner: "Reviewer"}}
		if err := packet.SetIntegrity(&p); err != nil {
			t.Fatal(err)
		}
		copy := roundTripPacket(t, p)
		for _, format := range []string{"html", "markdown"} {
			var before, after bytes.Buffer
			if err := report.Render(&before, p, &p, format); err != nil {
				t.Fatal(err)
			}
			if err := report.Render(&after, copy, &p, format); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before.Bytes(), after.Bytes()) {
				t.Fatalf("%s report changed after a packet round trip", format)
			}
			if format == "html" {
				for _, element := range []string{"<script", "<img", "<iframe", "<svg", "<form", "<object", "<embed"} {
					if strings.Contains(strings.ToLower(after.String()), element) {
						t.Fatalf("repository text created an active element: %s", element)
					}
				}
			}
		}
	})
}

func roundTripPacket(t *testing.T, p model.Packet) model.Packet {
	t.Helper()
	copy, err := packet.Decode(bytes.NewReader(encodeFixture(t, p)))
	if err != nil {
		t.Fatalf("accepted packet cannot be imported after encoding: %v", err)
	}
	if !reflect.DeepEqual(p, copy) {
		t.Fatal("packet data changed after encoding and importing")
	}
	result, err := comparison.Compare(p, copy)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 || result.Unchanged != len(p.Findings) || len(result.AssessmentChanges) != 0 || result.ContextChanged || result.DecisionChanged {
		t.Fatal("packet round trip introduced evidence or human-judgment changes")
	}
	return copy
}
