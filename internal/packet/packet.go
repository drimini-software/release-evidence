package packet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/drimini-software/release-evidence/internal/model"
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
	destination, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".release-evidence-packet-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := Encode(temporary, p, pretty); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}

	backupPath := ""
	if _, err := os.Stat(destination); err == nil {
		backup, createErr := os.CreateTemp(directory, ".release-evidence-previous-*")
		if createErr != nil {
			return fmt.Errorf("reserve previous-output path: %w", createErr)
		}
		backupPath = backup.Name()
		if closeErr := backup.Close(); closeErr != nil {
			return fmt.Errorf("close previous-output placeholder: %w", closeErr)
		}
		if removeErr := os.Remove(backupPath); removeErr != nil {
			return fmt.Errorf("prepare previous-output path: %w", removeErr)
		}
		if renameErr := os.Rename(destination, backupPath); renameErr != nil {
			return fmt.Errorf("preserve previous output: %w", renameErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output path: %w", err)
	}

	if err := os.Rename(temporaryPath, destination); err != nil {
		if backupPath != "" {
			_ = os.Rename(backupPath, destination)
		}
		return fmt.Errorf("replace output: %w", err)
	}

	if backupPath != "" {
		_ = os.Remove(backupPath)
	}
	return nil
}
