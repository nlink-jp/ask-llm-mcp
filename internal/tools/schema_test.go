package tools

import (
	"encoding/json"
	"testing"
)

// TestEveryToolSchemaIsClosed is the arch test organization ADR-021 §10
// requires: every registered tool's input schema sets
// additionalProperties:false, so a client validating arguments against the
// schema refuses a mistyped parameter instead of sending it on.
//
// The schema here already set it before this test existed; the test is what
// keeps it set, and what covers the next tool. It walks Registry — the same
// list cmd registers from — rather than naming ask_llm, so a tool added later
// is asserted over without anyone remembering to extend this file.
//
// Both halves of the contract are real in this server and each is tested: the
// schema stops the typo at the client, and the handler's
// DisallowUnknownFields stops it at the server
// (TestAskLLMHandler_RejectsUnknownField).
func TestEveryToolSchemaIsClosed(t *testing.T) {
	registry := Registry(nil)
	// Vacuity guard: with an empty list every assertion below passes without
	// having examined anything.
	if len(registry) == 0 {
		t.Fatal("Registry is empty, so this test proves nothing")
	}
	for _, r := range registry {
		var schema struct {
			Type                 string `json:"type"`
			AdditionalProperties *bool  `json:"additionalProperties"`
		}
		if err := json.Unmarshal(r.Tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q: input schema is not valid JSON: %v", r.Tool.Name, err)
			continue
		}
		if schema.Type != "object" {
			t.Errorf("tool %q: schema type = %q, want object", r.Tool.Name, schema.Type)
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			got := "absent"
			if schema.AdditionalProperties != nil {
				got = "true"
			}
			t.Errorf("tool %q: input schema does not set additionalProperties:false "+
				"(%s) — a validating client would pass an agent's mistyped argument "+
				"through unnoticed (organization ADR-021 §10)", r.Tool.Name, got)
		}
		if r.Tool.Description == "" {
			t.Errorf("tool %q has no description; it is all a client sees before calling",
				r.Tool.Name)
		}
	}
}

// TestRegistryProvidesAHandlerPerTool keeps Registry usable as the single
// registration list: a descriptor with no handler would register a tool that
// cannot be called.
func TestRegistryProvidesAHandlerPerTool(t *testing.T) {
	registry := Registry(&fakeAsker{})
	if len(registry) == 0 {
		t.Fatal("Registry is empty, so this test proves nothing")
	}
	for _, r := range registry {
		if r.Tool.Name == "" {
			t.Error("a registration has no tool name")
		}
		if r.Handler == nil {
			t.Errorf("tool %q has no handler", r.Tool.Name)
		}
	}
}
