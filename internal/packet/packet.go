package packet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/output"
)

type digestContent struct {
	Repository  model.Repository   `json:"repository"`
	Boundary    model.Boundary     `json:"boundary"`
	Complete    bool               `json:"complete"`
	Findings    []model.Finding    `json:"findings"`
	Diagnostics []model.Diagnostic `json:"diagnostics"`
}

// SetIntegrity records a reproducibility digest. It is not a signed attestation.
func SetIntegrity(p *model.Packet) error {
	content := digestContent{
		Repository:  p.Repository,
		Boundary:    p.Boundary,
		Complete:    p.Scan.Complete,
		Findings:    p.Findings,
		Diagnostics: p.Diagnostics,
	}

	encoded, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("encode integrity content: %w", err)
	}

	sum := sha256.Sum256(encoded)
	p.Integrity = model.Integrity{
		Algorithm: "sha256",
		Scope:     "normalized-evidence-v1",
		Digest:    hex.EncodeToString(sum[:]),
		Note:      "Reproducibility digest only; this packet is not cryptographically attested.",
	}
	return nil
}

func VerifyIntegrity(p model.Packet) error {
	stored := p.Integrity
	if err := SetIntegrity(&p); err != nil {
		return err
	}
	if stored.Algorithm != p.Integrity.Algorithm || stored.Scope != p.Integrity.Scope || stored.Digest != p.Integrity.Digest {
		return fmt.Errorf("packet integrity digest does not match its normalized evidence")
	}
	return nil
}

func Decode(reader io.Reader) (model.Packet, error) {
	content, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return model.Packet{}, fmt.Errorf("read packet: %w", err)
	}
	if len(content) > 4<<20 {
		return model.Packet{}, fmt.Errorf("packet exceeds the 4 MiB read limit")
	}
	if err := validateJSONMembers(content); err != nil {
		return model.Packet{}, fmt.Errorf("decode packet: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var result model.Packet
	if err := decoder.Decode(&result); err != nil {
		return model.Packet{}, fmt.Errorf("decode packet: %w", err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return model.Packet{}, fmt.Errorf("decode packet: trailing JSON content")
	}
	if err := model.ValidatePacket(result); err != nil {
		return model.Packet{}, fmt.Errorf("validate packet: %w", err)
	}
	if err := VerifyIntegrity(result); err != nil {
		return model.Packet{}, err
	}
	return result, nil
}

func Load(filePath string) (model.Packet, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return model.Packet{}, err
	}
	defer file.Close()
	return Decode(file)
}

func Encode(w io.Writer, p model.Packet, pretty bool) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(true)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(p); err != nil {
		return fmt.Errorf("encode packet: %w", err)
	}
	return nil
}

// WriteAtomic writes a packet without replacing the last valid file until the
// new content has been encoded and flushed successfully.
func WriteAtomic(destination string, p model.Packet, pretty bool) error {
	return output.WriteAtomic(destination, func(writer io.Writer) error {
		return Encode(writer, p, pretty)
	})
}
