package prompt

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Mode 表示 prompt 资产对应的运行模式。
type Mode string

const (
	// ModeAsk 表示 Ask 模式的静态资产。
	ModeAsk Mode = "ask"
	// ModePlan 表示 Plan 模式的静态资产。
	ModePlan Mode = "plan"
	// ModeAgent 表示 Agent 模式的静态资产。
	ModeAgent Mode = "agent"
	// ModeDebug 表示 Debug 模式的静态资产。
	ModeDebug Mode = "debug"
	// ModeMultitask 表示 Multitask 模式的静态资产。
	ModeMultitask Mode = "multitask"
	// ModeSubagent 表示子代理只读会话的静态资产。
	ModeSubagent Mode = "subagent"
)

// assetFS 保存按模式组织的静态 prompt 与 tools 资产。
//
//go:embed tools.json modes/*.json ask/prompt.md plan/prompt.md plan/system_reminder.txt agent/prompt.md debug/prompt.md debug/system_reminder_initial.txt debug/system_reminder_continuing.txt multitask/prompt.md subagent/prompt.md compaction/prompt.md commit/prompt.md
var assetFS embed.FS

var (
	cachedPrompts = make(map[Mode]string)
	cachedTools   = make(map[Mode][]byte)
	cachedDebugInitialReminder    string
	cachedDebugContinuingReminder string
	cachedPlanReminder            string
	cachedCompactionPrompt        string
	cachedCommitPrompt            string
)

type rawToolFunction struct {
	Name string `json:"name"`
}

type rawToolWrapper struct {
	Type     string          `json:"type"`
	Function rawToolFunction `json:"function"`
}

type toolsCatalogFile struct {
	Tools    []json.RawMessage          `json:"tools"`
	Variants map[string]json.RawMessage `json:"variants"`
}

type modeManifestTool struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
}

func (m *modeManifestTool) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if strings.Contains(s, ".") {
			parts := strings.SplitN(s, ".", 2)
			m.Name = parts[0]
			m.Variant = parts[1]
		} else {
			m.Name = s
		}
		return nil
	}
	type rawEntry modeManifestTool
	var raw rawEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = modeManifestTool(raw)
	return nil
}

type modeManifestFile struct {
	Tools []modeManifestTool `json:"tools"`
}

func assembleModeTools(mode Mode) ([]byte, error) {
	catalogData, err := assetFS.ReadFile("tools.json")
	if err != nil {
		return nil, fmt.Errorf("read tools.json: %w", err)
	}
	var catalog toolsCatalogFile
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		return nil, fmt.Errorf("unmarshal tools.json: %w", err)
	}

	canonicalTools := make(map[string]json.RawMessage, len(catalog.Tools))
	for _, item := range catalog.Tools {
		var wrapper rawToolWrapper
		if err := json.Unmarshal(item, &wrapper); err != nil || strings.TrimSpace(wrapper.Function.Name) == "" {
			continue
		}
		canonicalTools[wrapper.Function.Name] = item
	}

	variants := make(map[string]json.RawMessage, len(catalog.Variants))
	for key, item := range catalog.Variants {
		variants[strings.TrimSpace(key)] = item
	}

	manifestPath := fmt.Sprintf("modes/%s.json", mode)
	manifestData, err := assetFS.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read tool manifest %q: %w", manifestPath, err)
	}
	var manifest modeManifestFile
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal tool manifest %q: %w", manifestPath, err)
	}

	selectedTools := make([]json.RawMessage, 0, len(manifest.Tools))
	for _, toolEntry := range manifest.Tools {
		name := strings.TrimSpace(toolEntry.Name)
		if name == "" {
			continue
		}
		var toolDef json.RawMessage
		var found bool

		// 1. 显式指定的 variant (e.g. {name: "CreatePlan", variant: "plan"})
		if toolEntry.Variant != "" {
			variantKey := fmt.Sprintf("%s.%s", name, strings.TrimSpace(toolEntry.Variant))
			if v, ok := variants[variantKey]; ok {
				toolDef = v
				found = true
			}
		}

		// 2. 当前 mode 专属 variant (e.g. CreatePlan.plan)
		if !found {
			modeVariantKey := fmt.Sprintf("%s.%s", name, mode)
			if v, ok := variants[modeVariantKey]; ok {
				toolDef = v
				found = true
			}
		}

		// 3. 全局 canonical tool
		if !found {
			if t, ok := canonicalTools[name]; ok {
				toolDef = t
				found = true
			}
		}

		if !found {
			return nil, fmt.Errorf("mode %q references unknown tool %q", mode, name)
		}
		selectedTools = append(selectedTools, toolDef)
	}

	return json.Marshal(selectedTools)
}

func init() {
	for _, mode := range []Mode{ModeAsk, ModePlan, ModeAgent, ModeDebug, ModeMultitask, ModeSubagent} {
		path := fmt.Sprintf("%s/prompt.md", mode)
		data, err := assetFS.ReadFile(path)
		if err != nil {
			panic(fmt.Errorf("cache prompt %q: %w", path, err))
		}
		cachedPrompts[mode] = string(data)

		toolData, err := assembleModeTools(mode)
		if err != nil {
			panic(fmt.Errorf("assemble tools for mode %q: %w", mode, err))
		}
		cachedTools[mode] = toolData
	}

	if data, err := assetFS.ReadFile("debug/system_reminder_initial.txt"); err == nil {
		cachedDebugInitialReminder = strings.TrimSpace(string(data))
	}
	if data, err := assetFS.ReadFile("debug/system_reminder_continuing.txt"); err == nil {
		cachedDebugContinuingReminder = strings.TrimSpace(string(data))
	}
	if data, err := assetFS.ReadFile("plan/system_reminder.txt"); err == nil {
		cachedPlanReminder = strings.TrimSpace(string(data))
	}
	if data, err := assetFS.ReadFile("compaction/prompt.md"); err == nil {
		cachedCompactionPrompt = strings.TrimSpace(string(data))
	}
	if data, err := assetFS.ReadFile("commit/prompt.md"); err == nil {
		cachedCommitPrompt = strings.TrimSpace(string(data))
	}
}

// normalizeMode 校验并归一化传入的模式值。
func normalizeMode(mode Mode) (Mode, error) {
	switch mode {
	case ModeAsk, ModePlan, ModeAgent, ModeDebug, ModeMultitask, ModeSubagent:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported prompt mode: %q", mode)
	}
}

// PromptPath 返回指定模式的静态提示词资产路径。
func PromptPath(mode Mode) (string, error) {
	normalized, err := normalizeMode(mode)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/prompt.md", normalized), nil
}

// ToolsPath 返回指定模式的静态工具资产路径。
// ToolsPath 返回指定模式的静态工具清单资产路径。
func ToolsPath(mode Mode) (string, error) {
	normalized, err := normalizeMode(mode)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("modes/%s.json", normalized), nil
}

// ToolsCatalogPath 返回全局工具目录定义路径。
func ToolsCatalogPath() string {
	return "tools.json"
}

// ReadToolsCatalog 读取全局工具目录定义 JSON。
func ReadToolsCatalog() ([]byte, error) {
	return assetFS.ReadFile("tools.json")
}

// ReadPrompt 读取指定模式的静态提示词文本。
func ReadPrompt(mode Mode) (string, error) {
	normalized, err := normalizeMode(mode)
	if err != nil {
		return "", err
	}
	if prompt, ok := cachedPrompts[normalized]; ok {
		return prompt, nil
	}
	path := fmt.Sprintf("%s/prompt.md", normalized)
	data, err := assetFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt asset %q: %w", path, err)
	}
	return string(data), nil
}

// MustReadPrompt 读取指定模式的静态提示词文本，失败时直接 panic。
func MustReadPrompt(mode Mode) string {
	text, err := ReadPrompt(mode)
	if err != nil {
		panic(err)
	}
	return text
}

// ReadTools 读取指定模式组装后的工具 JSON 数组。
func ReadTools(mode Mode) ([]byte, error) {
	normalized, err := normalizeMode(mode)
	if err != nil {
		return nil, err
	}
	if tools, ok := cachedTools[normalized]; ok {
		return tools, nil
	}
	tools, err := assembleModeTools(normalized)
	if err != nil {
		return nil, err
	}
	cachedTools[normalized] = tools
	return tools, nil
}

// MustReadTools 读取指定模式的原始工具 JSON，失败时直接 panic。
func MustReadTools(mode Mode) []byte {
	data, err := ReadTools(mode)
	if err != nil {
		panic(err)
	}
	return data
}

// ReadDebugSystemReminder 读取 Debug 模式每轮追加的提醒资产。
func ReadDebugSystemReminder(initial bool) (string, error) {
	if initial && cachedDebugInitialReminder != "" {
		return cachedDebugInitialReminder, nil
	}
	if !initial && cachedDebugContinuingReminder != "" {
		return cachedDebugContinuingReminder, nil
	}
	path := "debug/system_reminder_continuing.txt"
	if initial {
		path = "debug/system_reminder_initial.txt"
	}
	data, err := assetFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read debug system reminder asset %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// MustReadDebugSystemReminder 读取 Debug 模式提醒资产，失败时直接 panic。
func MustReadDebugSystemReminder(initial bool) string {
	text, err := ReadDebugSystemReminder(initial)
	if err != nil {
		panic(err)
	}
	return text
}

// ReadPlanSystemReminder 读取 Plan 模式每轮追加的动态提醒资产。
func ReadPlanSystemReminder() (string, error) {
	if cachedPlanReminder != "" {
		return cachedPlanReminder, nil
	}
	const path = "plan/system_reminder.txt"
	data, err := assetFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read plan system reminder asset %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// MustReadPlanSystemReminder 读取 Plan 模式动态提醒资产，失败时直接 panic。
func MustReadPlanSystemReminder() string {
	text, err := ReadPlanSystemReminder()
	if err != nil {
		panic(err)
	}
	return text
}

// ReadCompactionPrompt 读取共享的压缩提示词资产。
func ReadCompactionPrompt() (string, error) {
	if cachedCompactionPrompt != "" {
		return cachedCompactionPrompt, nil
	}
	const path = "compaction/prompt.md"
	data, err := assetFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read compaction prompt asset %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// MustReadCompactionPrompt 读取共享的压缩提示词资产，失败时直接 panic。
func MustReadCompactionPrompt() string {
	text, err := ReadCompactionPrompt()
	if err != nil {
		panic(err)
	}
	return text
}

// ReadCommitPrompt 读取提交信息生成专用提示词资产。
func ReadCommitPrompt() (string, error) {
	if cachedCommitPrompt != "" {
		return cachedCommitPrompt, nil
	}
	const path = "commit/prompt.md"
	data, err := assetFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read commit prompt asset %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// MustReadCommitPrompt 读取提交信息生成专用提示词资产，失败时直接 panic。
func MustReadCommitPrompt() string {
	text, err := ReadCommitPrompt()
	if err != nil {
		panic(err)
	}
	return text
}
