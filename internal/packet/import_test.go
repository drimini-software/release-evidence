package packet_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/scanner"
)

func importFixture(tb testing.TB) model.Packet {
	tb.Helper()
	p, err := scanner.Scan(context.Background(), filepath.Join("..", "..", "fixtures", "session"), scanner.Options{ToolVersion: "import-test"})
	if err != nil {
		tb.Fatal(err)
	}
	p.Scan.GeneratedAt = "2026-10-11T00:00:00Z"
	return p
}

func encodeFixture(tb testing.TB, p model.Packet) []byte {
	tb.Helper()
	var encoded bytes.Buffer
	if err := packet.Encode(&encoded, p, false); err != nil {
		tb.Fatal(err)
	}
	return encoded.Bytes()
}

func TestDecodeRejectsAmbiguousObjectMembers(t *testing.T) {
	valid := string(encodeFixture(t, importFixture(t)))
	for name, input := range map[string]string{
		"top-level duplicate":         strings.Replace(valid, `"schema_version":"1"`, `"schema_version":"1","schema_version":"1"`, 1),
		"nested duplicate":            strings.Replace(valid, `"status":"not_made"`, `"status":"approved","status":"not_made"`, 1),
		"escaped duplicate":           strings.Replace(valid, `"status":"not_made"`, `"status":"approved","st\u0061tus":"not_made"`, 1),
		"top-level case alias":        strings.Replace(valid, `"schema_version"`, `"SCHEMA_VERSION"`, 1),
		"nested case alias":           strings.Replace(valid, `"status":"not_made"`, `"STATUS":"not_made"`, 1),
		"case alias overwrite":        strings.Replace(valid, `"status":"not_made"`, `"status":"approved","STATUS":"not_made"`, 1),
		"invalid UTF-8 in human text": strings.Replace(valid, `"status":"not_made"`, "\"status\":\"not_made\",\"rationale\":\"\xff\"", 1),
		"unpaired high surrogate":     strings.Replace(valid, `"status":"not_made"`, `"status":"not_made","rationale":"\ud800"`, 1),
		"unpaired low surrogate":      strings.Replace(valid, `"status":"not_made"`, `"status":"not_made","rationale":"\udfff"`, 1),
		"reversed surrogate pair":     strings.Replace(valid, `"status":"not_made"`, `"status":"not_made","rationale":"\udc00\ud800"`, 1),
		"non-surrogate second half":   strings.Replace(valid, `"status":"not_made"`, `"status":"not_made","rationale":"\ud800\u0041"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if input == valid {
				t.Fatal("test did not mutate the fixture")
			}
			if _, err := packet.Decode(strings.NewReader(input)); err == nil {
				t.Fatal("ambiguous or damaged JSON packet was accepted")
			}
		})
	}
}

func TestDecodePreservesValidUnicodeAndLiteralEscapes(t *testing.T) {
	valid := string(encodeFixture(t, importFixture(t)))
	for encoded, expected := range map[string]string{
		`\ud83d\ude00`:                    "😀",
		`\uD83D\uDE00`:                    "😀",
		`\ufffd`:                          "\ufffd",
		`日本語 café`:                        "日本語 café",
		`\\ud800`:                         `\ud800`,
		`escaped quote: \" and slash: \\`: "escaped quote: \" and slash: \\",
	} {
		t.Run(encoded, func(t *testing.T) {
			input := strings.Replace(valid, `"status":"not_made"`, `"status":"not_made","rationale":"`+encoded+`"`, 1)
			p, err := packet.Decode(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if p.Decision.Rationale != expected {
				t.Fatalf("valid Unicode text changed: got %q, want %q", p.Decision.Rationale, expected)
			}
		})
	}
}

func TestDecodeAcceptsEscapedCanonicalMemberNames(t *testing.T) {
	valid := string(encodeFixture(t, importFixture(t)))
	input := strings.Replace(valid, `"schema_version"`, `"schema_versi\u006fn"`, 1)
	if _, err := packet.Decode(strings.NewReader(input)); err != nil {
		t.Fatalf("canonical escaped member name rejected: %v", err)
	}
}

func TestDecodeRejectsDeeplyNestedJSON(t *testing.T) {
	input := strings.Repeat(`[`, 100) + `0` + strings.Repeat(`]`, 100)
	if _, err := packet.Decode(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "nesting limit") {
		t.Fatalf("deep nesting was not rejected at the import boundary: %v", err)
	}
}

func TestDecodeRejectsDuplicateMemberInsideArray(t *testing.T) {
	valid := string(encodeFixture(t, importFixture(t)))
	input := strings.Replace(valid, `"rule_version":"1"`, `"rule_version":"1","rule_version":"1"`, 1)
	if input == valid {
		t.Fatal("test did not mutate the fixture")
	}
	if _, err := packet.Decode(strings.NewReader(input)); err == nil {
		t.Fatal("duplicate member in findings array was accepted")
	}
}
