// tool_catalog.go 负责从静态 prompt 资产中装载并筛选 canonical tool catalog。
package forwarder

import (
	"encoding/json"
	"fmt"
	"strings"

	"cursor/gen/agentv1"
	promptassets "cursor/prompt"
)

type cachedToolCatalogEntry struct {
	items []json.RawMessage
	names []string
}

var (
	cachedToolCatalogs = make(map[promptassets.Mode]cachedToolCatalogEntry)
)

func init() {
	for _, mode := range []promptassets.Mode{
		promptassets.ModeAsk,
		promptassets.ModePlan,
		promptassets.ModeAgent,
		promptassets.ModeDebug,
		promptassets.ModeMultitask,
		promptassets.ModeSubagent,
	} {
		rawTools, err := promptassets.ReadTools(mode)
		if err != nil {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(rawTools, &items); err != nil {
			continue
		}
		names := make([]string, 0, len(items))
		for _, item := range items {
			name, err := extractToolName(item)
			if err != nil {
				continue
			}
			names = append(names, name)
		}
		cachedToolCatalogs[mode] = cachedToolCatalogEntry{
			items: items,
			names: names,
		}
	}
}

type DefaultToolCatalog struct {
}

// NewToolCatalog 创建默认工具目录实现。
func NewToolCatalog() *DefaultToolCatalog {
	return &DefaultToolCatalog{}
}

// Load 按 mode 读取工具资产，并过滤出当前阶段真正允许暴露的工具。
func (catalog *DefaultToolCatalog) Load(mode agentv1.AgentMode, subagentTypeName string) ([]json.RawMessage, []string, error) {
	assetMode, err := toolAssetModeForConversation(mode, subagentTypeName)
	if err != nil {
		return nil, nil, err
	}
	cached, ok := cachedToolCatalogs[assetMode]
	if !ok {
		rawTools, err := promptassets.ReadTools(assetMode)
		if err != nil {
			return nil, nil, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(rawTools, &items); err != nil {
			return nil, nil, fmt.Errorf("decode tools asset failed: %w", err)
		}
		cached = cachedToolCatalogEntry{items: items}
		for _, item := range items {
			name, err := extractToolName(item)
			if err != nil {
				return nil, nil, err
			}
			cached.names = append(cached.names, name)
		}
	}

	filtered := make([]json.RawMessage, 0, len(cached.items))
	names := make([]string, 0, len(cached.names))
	for i, item := range cached.items {
		name := cached.names[i]
		if !isToolAllowedInMode(mode, subagentTypeName, name) {
			continue
		}
		filtered = append(filtered, item)
		names = append(names, name)
	}
	return filtered, names, nil
}

var agentModeToolNames = map[string]struct{}{
	"AskQuestion":          {},
	"CallMcpTool":          {},
	"CreatePlan":           {},
	"Delete":               {},
	"FetchMcpResource":     {},
	"GenerateImage":        {},
	"Glob":                 {},
	"Grep":                 {},
	"Ls":                   {},
	"PatchEdit":            {},
	"Read":                 {},
	"ReadLints":            {},
	"Shell":                {},
	"AwaitShell":           {},
	"WriteShellStdin":      {},
	"ForceBackgroundShell": {},
	"SwitchMode":           {},
	"Task":                 {},
	"TodoWrite":            {},
	"WebFetch":             {},
	"WebSearch":            {},
	"Write":                {},
}

var multitaskModeToolNames = map[string]struct{}{
	"AskQuestion":          {},
	"CallMcpTool":          {},
	"Delete":               {},
	"FetchMcpResource":     {},
	"GenerateImage":        {},
	"Glob":                 {},
	"Grep":                 {},
	"Ls":                   {},
	"PatchEdit":            {},
	"Read":                 {},
	"ReadLints":            {},
	"Shell":                {},
	"AwaitShell":           {},
	"WriteShellStdin":      {},
	"ForceBackgroundShell": {},
	"SwitchMode":           {},
	"Task":                 {},
	"TodoWrite":            {},
	"WebFetch":             {},
	"WebSearch":            {},
	"Write":                {},
}

var debugModeToolNames = map[string]struct{}{
	"AskQuestion":          {},
	"CallMcpTool":          {},
	"Delete":               {},
	"FetchMcpResource":     {},
	"Glob":                 {},
	"Grep":                 {},
	"Ls":                   {},
	"PatchEdit":            {},
	"Read":                 {},
	"ReadLints":            {},
	"Shell":                {},
	"AwaitShell":           {},
	"WriteShellStdin":      {},
	"ForceBackgroundShell": {},
	"Task":                 {},
	"TodoWrite":            {},
	"WebFetch":             {},
	"WebSearch":            {},
	"Write":                {},
}

var askModeToolNames = map[string]struct{}{
	"AskQuestion":          {},
	"CallMcpTool":          {},
	"Delete":               {},
	"FetchMcpResource":     {},
	"Glob":                 {},
	"Grep":                 {},
	"Ls":                   {},
	"PatchEdit":            {},
	"Read":                 {},
	"ReadLints":            {},
	"Shell":                {},
	"AwaitShell":           {},
	"WriteShellStdin":      {},
	"ForceBackgroundShell": {},
	"Task":                 {},
	"TodoWrite":            {},
	"WebFetch":             {},
	"WebSearch":            {},
	"Write":                {},
}

var planModeToolNames = map[string]struct{}{
	"AskQuestion":          {},
	"CallMcpTool":          {},
	"CreatePlan":           {},
	"Delete":               {},
	"FetchMcpResource":     {},
	"Glob":                 {},
	"Grep":                 {},
	"Ls":                   {},
	"PatchEdit":            {},
	"Read":                 {},
	"ReadLints":            {},
	"Shell":                {},
	"AwaitShell":           {},
	"WriteShellStdin":      {},
	"ForceBackgroundShell": {},
	"Task":                 {},
	"TodoWrite":            {},
	"WebFetch":             {},
	"WebSearch":            {},
	"Write":                {},
}

var childConversationDisallowedAgentToolNames = map[string]struct{}{
	"AskQuestion": {},
}

func supportedToolNamesForMode(mode agentv1.AgentMode) map[string]struct{} {
	switch normalizeMode(mode) {
	case agentv1.AgentMode_AGENT_MODE_AGENT:
		return agentModeToolNames
	case agentv1.AgentMode_AGENT_MODE_ASK:
		return askModeToolNames
	case agentv1.AgentMode_AGENT_MODE_PLAN:
		return planModeToolNames
	case agentv1.AgentMode_AGENT_MODE_DEBUG:
		return debugModeToolNames
	case agentv1.AgentMode_AGENT_MODE_MULTITASK:
		return multitaskModeToolNames
	default:
		return nil
	}
}

func isKnownToolName(toolName string) bool {
	trimmedToolName := strings.TrimSpace(toolName)
	if trimmedToolName == "" {
		return false
	}
	for _, supported := range []map[string]struct{}{
		agentModeToolNames,
		askModeToolNames,
		planModeToolNames,
		debugModeToolNames,
		multitaskModeToolNames,
	} {
		if _, ok := supported[trimmedToolName]; ok {
			return true
		}
	}
	return false
}

func isToolAllowedInMode(mode agentv1.AgentMode, subagentTypeName string, toolName string) bool {
	trimmedToolName := strings.TrimSpace(toolName)
	if trimmedToolName == "" {
		return false
	}
	if isChildConversationSubagentTypeName(subagentTypeName) {
		if _, disallowed := childConversationDisallowedAgentToolNames[trimmedToolName]; disallowed {
			return false
		}
		_, ok := agentModeToolNames[trimmedToolName]
		return ok
	}
	supported := supportedToolNamesForMode(mode)
	if supported == nil {
		return false
	}
	_, ok := supported[trimmedToolName]
	return ok
}

func isChildConversationSubagentTypeName(subagentTypeName string) bool {
	return strings.TrimSpace(subagentTypeName) != ""
}

func selectToolsByOrderedNames(items []json.RawMessage, orderedNames []string) ([]json.RawMessage, []string, error) {
	byName := make(map[string]json.RawMessage, len(items))
	for _, item := range items {
		name, err := extractToolName(item)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := byName[name]; !exists {
			byName[name] = item
		}
	}
	filtered := make([]json.RawMessage, 0, len(orderedNames))
	names := make([]string, 0, len(orderedNames))
	for _, name := range orderedNames {
		item, ok := byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("tool descriptor %q not found in prompt asset", name)
		}
		filtered = append(filtered, item)
		names = append(names, name)
	}
	return filtered, names, nil
}

func toolAssetModeForConversation(mode agentv1.AgentMode, subagentTypeName string) (promptassets.Mode, error) {
	if isChildConversationSubagentTypeName(subagentTypeName) {
		return promptassets.ModeAgent, nil
	}
	return mapPromptMode(mode)
}

func promptAssetModeForConversation(mode agentv1.AgentMode, subagentTypeName string) (promptassets.Mode, error) {
	if isChildConversationSubagentTypeName(subagentTypeName) {
		return promptassets.ModeSubagent, nil
	}
	return mapPromptMode(mode)
}

// mapPromptMode 把协议 mode 映射为静态 prompt 资产对应的目录名。
func mapPromptMode(mode agentv1.AgentMode) (promptassets.Mode, error) {
	switch normalizeMode(mode) {
	case agentv1.AgentMode_AGENT_MODE_AGENT:
		return promptassets.ModeAgent, nil
	case agentv1.AgentMode_AGENT_MODE_ASK:
		return promptassets.ModeAsk, nil
	case agentv1.AgentMode_AGENT_MODE_PLAN:
		return promptassets.ModePlan, nil
	case agentv1.AgentMode_AGENT_MODE_DEBUG:
		return promptassets.ModeDebug, nil
	case agentv1.AgentMode_AGENT_MODE_MULTITASK:
		return promptassets.ModeMultitask, nil
	default:
		return "", fmt.Errorf("unsupported prompt asset mode: %s", mode.String())
	}
}

// extractToolName 从原始 tool descriptor JSON 中提取函数名。
func extractToolName(raw json.RawMessage) (string, error) {
	var wrapper struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return "", fmt.Errorf("decode tool descriptor failed: %w", err)
	}
	name := strings.TrimSpace(wrapper.Function.Name)
	if name == "" {
		return "", fmt.Errorf("tool descriptor name is required")
	}
	return name, nil
}

// sanitizePromptAsset 去掉资产文件中的说明性标题，只保留真正的 prompt 文本。
func sanitizePromptAsset(text string, modelName string) string {
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "# 通用系统提示词", "# 模式静态补充", "---":
			continue
		default:
			filtered = append(filtered, line)
		}
	}
	return promptassets.RenderPromptTemplate(strings.TrimSpace(strings.Join(filtered, "\n")), modelName)
}
