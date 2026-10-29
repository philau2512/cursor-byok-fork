package upstream

import (
	"encoding/json"
	"reflect"
	"testing"

	"cursor/gen/agentv1"
	legacyruntime "cursor/internal/runtime"

	"google.golang.org/protobuf/proto"
)

func TestBuildCLIModelDetailsPreservesChannelMetadata(t *testing.T) {
	adapters := []legacyruntime.ModelAdapterConfig{
		{ID: " channel-a ", DisplayName: " Model A ", ModelID: "model-a", APIKey: "provider-secret-a", BaseURL: "https://provider-a.example/v1"},
		{ID: "channel-b", DisplayName: "Model B", ModelID: "model-a"},
		{ID: "", ModelID: "model-c"},
	}

	got := buildCLIModelDetails(adapters)
	want := []map[string]any{
		{"modelId": "channel-a", "displayModelId": "channel-a", "displayName": "Model A", "displayNameShort": "Model A", "apiKeyCredentials": map[string]any{"apiKey": "provider-secret-a", "baseUrl": "https://provider-a.example/v1"}},
		{"modelId": "channel-b", "displayModelId": "channel-b", "displayName": "Model B", "displayNameShort": "Model B", "apiKeyCredentials": map[string]any{"apiKey": "", "baseUrl": ""}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("build CLI model details: got %v, want %v", got, want)
	}
}

func TestDefaultThinkingEffortForOpenAIAdapterUsesDisabledWhenUnset(t *testing.T) {
	adapter := legacyruntime.ModelAdapterConfig{Type: "openai", ReasoningEffort: ""}

	if got := defaultThinkingEffortForAdapter(adapter); got != "disabled" {
		t.Fatalf("default thinking effort = %q, want disabled", got)
	}
}

func TestBuildAvailableModelEntriesUsesDisabledVariantWhenReasoningEffortUnset(t *testing.T) {
	entries := buildAvailableModelEntries([]legacyruntime.ModelAdapterConfig{{
		ID:          "channel-a",
		DisplayName: "Model A",
		ModelID:     "model-a",
		Type:        "openai",
	}})
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}

	variants, ok := entries[0]["variants"].([]map[string]any)
	if !ok {
		t.Fatalf("variants type = %T, want []map[string]any", entries[0]["variants"])
	}
	if len(variants) == 0 {
		t.Fatal("variants should not be empty")
	}
	if got := variants[0]["variantStringRepresentation"]; got != "channel-a:disabled" {
		t.Fatalf("first variant representation = %#v, want channel-a:disabled", got)
	}
	if got := variants[0]["isDefaultNonMaxConfig"]; got != true {
		t.Fatalf("disabled variant default flag = %#v, want true", got)
	}
}

func TestEncodeCLIModelsUsesAgentModelDetailsWireFormat(t *testing.T) {
	payload := map[string]any{"models": buildCLIModelDetails([]legacyruntime.ModelAdapterConfig{{ID: "channel-a", DisplayName: "Model A", APIKey: "provider-secret", BaseURL: "https://provider.example/v1"}})}
	encoded, err := encodeMockProto("aiserver.v1.GetUsableModelsResponse", payload)
	if err != nil {
		t.Fatalf("encode CLI models: %v", err)
	}

	response := &agentv1.GetUsableModelsResponse{}
	if err := proto.Unmarshal(encoded, response); err != nil {
		t.Fatalf("decode CLI models with agent proto: %v", err)
	}
	if len(response.Models) != 1 {
		t.Fatalf("decoded model count: got %d, want 1", len(response.Models))
	}
	model := response.Models[0]
	if model.GetModelId() != "channel-a" || model.GetDisplayModelId() != "channel-a" {
		t.Fatalf("decoded channel IDs: model=%q display=%q", model.GetModelId(), model.GetDisplayModelId())
	}
	if model.GetDisplayName() != "Model A" || model.GetDisplayNameShort() != "Model A" {
		t.Fatalf("decoded display names: name=%q short=%q", model.GetDisplayName(), model.GetDisplayNameShort())
	}
	if credentials := model.GetApiKeyCredentials(); credentials == nil || credentials.GetApiKey() != "provider-secret" || credentials.GetBaseUrl() != "https://provider.example/v1" {
		t.Fatalf("decoded relay credentials: %#v", credentials)
	}
}

func TestBuildBootstrapStatsigConfigJSONDisablesAlwaysLocalDecompositionGate(t *testing.T) {
	payload, err := buildBootstrapStatsigConfigJSON(12345, "test-auth-id")
	if err != nil {
		t.Fatalf("build bootstrap statsig config: %v", err)
	}

	var decoded statsigBootstrapTemplate
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode bootstrap statsig config: %v", err)
	}

	gate, ok := decoded.FeatureGates[bootstrapStatsigDecomposeAlwaysLocalExtHostGate]
	if !ok {
		t.Fatalf("missing feature gate %q", bootstrapStatsigDecomposeAlwaysLocalExtHostGate)
	}
	if value, _ := gate["value"].(bool); value {
		t.Fatalf("expected %q to be disabled", bootstrapStatsigDecomposeAlwaysLocalExtHostGate)
	}
	if ruleID, _ := gate["rule_id"].(string); ruleID != "local_disabled" {
		t.Fatalf("unexpected rule_id: %q", ruleID)
	}
}

func TestBuildBootstrapStatsigConfigJSONEnablesTerminalOutputUIStreaming(t *testing.T) {
	payload, err := buildBootstrapStatsigConfigJSON(12345, "test-auth-id")
	if err != nil {
		t.Fatalf("build bootstrap statsig config: %v", err)
	}

	var decoded statsigBootstrapTemplate
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode bootstrap statsig config: %v", err)
	}

	gate, ok := decoded.FeatureGates[bootstrapStatsigDisableTerminalOutputUIStreaming]
	if !ok {
		t.Fatalf("missing feature gate %q", bootstrapStatsigDisableTerminalOutputUIStreaming)
	}
	if value, _ := gate["value"].(bool); value {
		t.Fatalf("expected %q to be disabled", bootstrapStatsigDisableTerminalOutputUIStreaming)
	}
	if ruleID, _ := gate["rule_id"].(string); ruleID != "local_disabled" {
		t.Fatalf("unexpected rule_id: %q", ruleID)
	}
}

func TestBuildAvailableModelEntriesIncludesContextPresetsAndCustom(t *testing.T) {
	adapters := []legacyruntime.ModelAdapterConfig{
		{
			ID:                  "standard-model",
			DisplayName:         "Standard Model",
			ModelID:             "gpt-4o",
			Type:                "openai",
			ContextWindowTokens: 0,
		},
		{
			ID:                  "custom-model",
			DisplayName:         "Custom Model",
			ModelID:             "claude-3-7-sonnet",
			Type:                "anthropic",
			ContextWindowTokens: 356000,
		},
		{
			ID:                  "arbitrary-model",
			DisplayName:         "Arbitrary Model",
			ModelID:             "deepseek-r1",
			Type:                "openai",
			ContextWindowTokens: 128000,
		},
	}

	entries := buildAvailableModelEntries(adapters)
	if len(entries) != 3 {
		t.Fatalf("entry count = %d, want 3", len(entries))
	}

	// 1. Standard model should have context parameter with 200k, 356k, 500k, 800k, 1m
	standardParams, ok := entries[0]["parameterDefinitions"].([]map[string]any)
	if !ok || len(standardParams) < 2 {
		t.Fatalf("standard parameterDefinitions = %#v", entries[0]["parameterDefinitions"])
	}
	if standardParams[0]["id"] != "context" {
		t.Errorf("first parameter id = %q, want context", standardParams[0]["id"])
	}
	enumParam, _ := standardParams[0]["parameterType"].(map[string]any)["enumParameter"].(map[string]any)
	contextVals, _ := enumParam["values"].([]map[string]any)
	if len(contextVals) != 5 {
		t.Errorf("standard context options count = %d, want 5", len(contextVals))
	}

	// 2. Custom model with 356000 matches 356k preset
	customVariants, _ := entries[1]["variants"].([]map[string]any)
	if len(customVariants) == 0 {
		t.Fatal("customVariants is empty")
	}
	firstCustomParamVals := customVariants[0]["parameterValues"].([]map[string]any)
	if firstCustomParamVals[0]["value"] != "356k" {
		t.Errorf("default context variant for 356000 tokens = %v, want 356k", firstCustomParamVals[0]["value"])
	}

	// 3. Arbitrary model with 128000 gets a custom context option
	arbitraryParams, _ := entries[2]["parameterDefinitions"].([]map[string]any)
	arbEnumParam, _ := arbitraryParams[0]["parameterType"].(map[string]any)["enumParameter"].(map[string]any)
	arbVals, _ := arbEnumParam["values"].([]map[string]any)
	if len(arbVals) != 6 {
		t.Errorf("arbitrary model context options count = %d, want 6 (5 presets + 1 custom)", len(arbVals))
	}
	if arbVals[0]["value"] != "128k" {
		t.Errorf("arbitrary custom option value = %v, want 128k", arbVals[0]["value"])
	}
	if arbVals[0]["displayName"] != "128K (Custom)" {
		t.Errorf("arbitrary custom option displayName = %v, want 128K (Custom)", arbVals[0]["displayName"])
	}
}
