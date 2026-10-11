package packet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// The schema uses lowercase ASCII member names. encoding/json's legacy
// decoding also accepts case aliases, overwrites duplicate members, and repairs
// invalid UTF-8. Reject those representations before decoding recorded facts.
// Values still use the standard parser and the existing typed/schema checks.
func validateJSONMembers(content []byte) error {
	if !utf8.Valid(content) {
		return fmt.Errorf("packet JSON is not valid UTF-8")
	}
	if !validUnicodeEscapes(content) {
		return fmt.Errorf("packet JSON contains an invalid Unicode escape")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	// Valid schema-v1 packets are much shallower. Bound hostile nesting before
	// recursive inspection, including arrays in otherwise invalid fields.
	if depth > 32 {
		return fmt.Errorf("packet JSON exceeds the nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid packet JSON")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("invalid packet JSON member")
			}
			name, ok := token.(string)
			if !ok || !canonicalMemberName(name) {
				return fmt.Errorf("packet JSON member names must use lowercase ASCII schema spelling")
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("packet JSON contains duplicate object members")
			}
			seen[name] = struct{}{}
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid packet JSON container")
	}
	closing, err := decoder.Token()
	if err != nil || (delimiter == '{' && closing != json.Delim('}')) || (delimiter == '[' && closing != json.Delim(']')) {
		return fmt.Errorf("invalid packet JSON container")
	}
	return nil
}

func canonicalMemberName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character != '_' && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

// encoding/json also substitutes U+FFFD for unpaired UTF-16 surrogate escapes.
// Inspect escapes inside strings so damaged human text cannot be repaired
// silently. Ordinary escapes and literal U+FFFD remain valid JSON input.
func validUnicodeEscapes(content []byte) bool {
	inString := false
	for index := 0; index < len(content); index++ {
		if content[index] == '"' {
			inString = !inString
		} else if inString && content[index] == '\\' {
			index++
			if index >= len(content) {
				return false
			}
			if content[index] != 'u' {
				continue
			}
			if index+4 >= len(content) {
				return false
			}
			unit, err := strconv.ParseUint(string(content[index+1:index+5]), 16, 16)
			if err != nil {
				return false
			}
			index += 4
			if !utf16.IsSurrogate(rune(unit)) {
				continue
			}
			if unit > 0xdbff || index+6 >= len(content) || content[index+1] != '\\' || content[index+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(content[index+3:index+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}
