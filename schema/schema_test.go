package schema_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPacketSchemaIsValidJSON(t *testing.T) {
	content, err := os.ReadFile("packet.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatalf("packet schema is not valid JSON: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatal("packet schema does not declare JSON Schema 2020-12")
	}
	if schema["title"] != "Release Evidence packet v1" {
		t.Fatal("packet schema title changed unexpectedly")
	}
}
