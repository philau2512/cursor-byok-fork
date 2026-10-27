package forwarder

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"

	"cursor/gen/agentv1"
	"cursor/gen/aiserverv1"
	modeladapter "cursor/internal/backend/agent/model"
	promptengine "cursor/internal/backend/agent/prompt"
)

const (
	estimatedTokensPerMessageOverhead  = int64(8)
	estimatedTokensPerToolCallOverhead = int64(6)
	estimatedTokensPerImagePart        = int64(1024)
)

func estimateCompiledPromptTokens(compiled CompiledConversation) int64 {
	return estimateModelMessagesTokens(compiled.Messages) + estimateToolDescriptorsTokens(compiled.Tools)
}

func estimateModelMessagesTokens(messages []modeladapter.Message) int64 {
	total := int64(0)
	for _, item := range messages {
		total += estimateModelMessageTokens(item)
	}
	return total
}

func estimateModelMessageTokens(item modeladapter.Message) int64 {
	total := estimatedTokensPerMessageOverhead
	total += estimateTextTokens(item.Role)
	total += estimateTextTokens(item.Content)
	total += estimateModelContentPartsTokens(item.Content, item.ContentParts)
	total += estimateTextTokens(item.ReasoningContent)
	total += estimateTextTokens(item.ReasoningSignature)
	total += estimateTextTokens(item.ToolCallID)
	total += estimateTextTokens(item.Name)
	for _, toolCall := range item.ToolCalls {
		total += estimatedTokensPerToolCallOverhead
		total += estimateTextTokens(toolCall.ID)
		total += estimateTextTokens(toolCall.Type)
		total += estimateTextTokens(toolCall.Function.Name)
		total += estimateTextTokens(toolCall.Function.Arguments)
	}
	return total
}

func estimateToolDescriptorsTokens(tools []json.RawMessage) int64 {
	total := int64(0)
	for _, item := range tools {
		total += estimateTextTokens(string(item))
	}
	return total
}

func estimateContextItemTokens(item *aiserverv1.ContextItem) int64 {
	if item == nil {
		return 0
	}
	body, err := protojson.MarshalOptions{EmitUnpopulated: false}.Marshal(item)
	if err != nil {
		return estimateTextTokens(item.String())
	}
	return estimateTextTokens(string(body))
}

func estimateTextTokens(text string) int64 {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount <= 0 {
		return 0
	}
	estimated := int64((runeCount + 3) / 4)
	estimated += int64(strings.Count(trimmed, "\n"))
	if estimated < 1 {
		return 1
	}
	return estimated
}

func estimateModelContentPartsTokens(content string, parts []modeladapter.ContentPart) int64 {
	if len(parts) == 0 {
		return 0
	}
	total := int64(0)
	countText := strings.TrimSpace(content) == ""
	for _, part := range parts {
		switch strings.TrimSpace(strings.ToLower(part.Type)) {
		case "", "text":
			if countText {
				total += estimateTextTokens(part.Text)
			}
		case "image":
			total += estimatedTokensPerImagePart
			if part.Image != nil {
				total += estimateTextTokens(part.Image.MIMEType)
				total += estimateTextTokens(part.Image.Path)
			}
		}
	}
	return total
}

func estimatePromptContentPartsTokens(content string, parts []promptengine.ContentPart) int64 {
	if len(parts) == 0 {
		return 0
	}
	total := int64(0)
	countText := strings.TrimSpace(content) == ""
	for _, part := range parts {
		switch strings.TrimSpace(strings.ToLower(part.Type)) {
		case "", "text":
			if countText {
				total += estimateTextTokens(part.Text)
			}
		case "image":
			total += estimatedTokensPerImagePart
			if part.Image != nil {
				total += estimateTextTokens(part.Image.MIMEType)
				total += estimateTextTokens(part.Image.Path)
			}
		}
	}
	return total
}

const (
	breakdownCatSystemPrompt = 0
	breakdownCatTools        = 1
	breakdownCatRules        = 2
	breakdownCatSkills       = 3
	breakdownCatMCP          = 4
	breakdownCatSubagents    = 5
	breakdownCatSummary      = 6
	breakdownCatConversation = 7
	breakdownCatCount        = 8
)

var breakdownCategoryDefinitions = [breakdownCatCount]struct {
	id    string
	label string
}{
	{"system_prompt", "System prompt"},
	{"tools", "Tool definitions"},
	{"rules", "Rules"},
	{"skills", "Skills"},
	{"mcp", "MCP & dynamic tools"},
	{"subagents", "Subagent definitions"},
	{"summarized_conversation", "Summarized conversation"},
	{"conversation", "Conversation"},
}

type breakdownMeasure struct {
	characters     int64
	newlines       int64
	overheadTokens int64
}

func (m *breakdownMeasure) add(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	m.characters += int64(utf8.RuneCountInString(trimmed))
	m.newlines += int64(strings.Count(trimmed, "\n"))
}

func (m *breakdownMeasure) addOverhead(tokens int64) {
	m.overheadTokens += tokens
}

func (m breakdownMeasure) estimatedTokens() int64 {
	if m.characters == 0 && m.overheadTokens == 0 {
		return 0
	}
	textTokens := int64(0)
	if m.characters > 0 {
		runeBased := (m.characters + 3) / 4
		total := runeBased + m.newlines
		if total < 1 {
			total = 1
		}
		textTokens = total
	}
	return textTokens + m.overheadTokens
}

type breakdownTagRange struct {
	start    int
	end      int
	category int
}

func collectTagRanges(text string, tag string, category int, output *[]breakdownTagRange) {
	opening := "<" + tag
	closing := "</" + tag + ">"
	cursor := 0
	for {
		relativeStart := strings.Index(text[cursor:], opening)
		if relativeStart == -1 {
			break
		}
		start := cursor + relativeStart
		openEndRel := strings.Index(text[start:], ">")
		if openEndRel == -1 {
			break
		}
		openEnd := start + openEndRel + 1
		relativeEnd := strings.Index(text[openEnd:], closing)
		if relativeEnd == -1 {
			break
		}
		end := openEnd + relativeEnd + len(closing)
		*output = append(*output, breakdownTagRange{
			start:    start,
			end:      end,
			category: category,
		})
		cursor = end
	}
}

func measureSystemPrompt(text string, measures *[breakdownCatCount]breakdownMeasure) {
	var ranges []breakdownTagRange
	for _, tag := range []string{
		"shared_user_rules",
		"rules",
		"rule",
		"user_rules",
		"always_applied_workspace_rules",
		"agent_requestable_workspace_rules",
		"cursor_rules_context",
	} {
		collectTagRanges(text, tag, breakdownCatRules, &ranges)
	}
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})

	cursor := 0
	for _, r := range ranges {
		if r.start < cursor {
			continue
		}
		measures[breakdownCatSystemPrompt].add(text[cursor:r.start])
		measures[r.category].add(text[r.start:r.end])
		cursor = r.end
	}
	measures[breakdownCatSystemPrompt].add(text[cursor:])
}

func measureRuntimeText(text string, measures *[breakdownCatCount]breakdownMeasure, measuredStaticCategories *[breakdownCatCount]bool) {
	var ranges []breakdownTagRange

	for _, tag := range []string{
		"shared_user_rules",
		"rules",
		"rule",
		"user_rule",
		"user_rules",
		"always_applied_workspace_rules",
		"agent_requestable_workspace_rules",
		"cursor_rules_context",
	} {
		collectTagRanges(text, tag, breakdownCatRules, &ranges)
	}
	for _, tag := range []string{
		"agent_skills",
		"available_skills",
		"skill",
	} {
		collectTagRanges(text, tag, breakdownCatSkills, &ranges)
	}
	collectTagRanges(text, "subagents", breakdownCatSubagents, &ranges)
	for _, tag := range []string{
		"mcp_meta_tools",
		"mcp_instructions",
		"mcp_file_system",
		"mcp_file_system_servers",
		"mcp_descriptors",
	} {
		collectTagRanges(text, tag, breakdownCatMCP, &ranges)
	}
	for _, tag := range []string{
		"conversation_summary",
		"summary_of_previous_conversation",
	} {
		collectTagRanges(text, tag, breakdownCatSummary, &ranges)
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})

	cursor := 0
	for _, r := range ranges {
		if r.start < cursor {
			continue
		}
		measures[breakdownCatConversation].add(text[cursor:r.start])
		chunk := text[r.start:r.end]
		if r.category == breakdownCatRules || r.category == breakdownCatSkills || r.category == breakdownCatSubagents || r.category == breakdownCatMCP {
			if !measuredStaticCategories[r.category] {
				measures[r.category].add(chunk)
				measuredStaticCategories[r.category] = true
			}
		} else {
			measures[r.category].add(chunk)
		}
		cursor = r.end
	}
	measures[breakdownCatConversation].add(text[cursor:])
}

func measureBreakdownMessage(message modeladapter.Message, measures *[breakdownCatCount]breakdownMeasure, measuredStaticCategories *[breakdownCatCount]bool) {
	measures[breakdownCatConversation].addOverhead(estimatedTokensPerMessageOverhead)
	role := strings.TrimSpace(message.Role)
	if role == "system" {
		measureSystemPrompt(message.Content, measures)
		return
	}

	if len(message.ContentParts) > 0 {
		for _, part := range message.ContentParts {
			switch strings.TrimSpace(strings.ToLower(part.Type)) {
			case "", "text":
				measureRuntimeText(part.Text, measures, measuredStaticCategories)
			case "image":
				measures[breakdownCatConversation].addOverhead(estimatedTokensPerImagePart)
				if part.Image != nil {
					measures[breakdownCatConversation].add(part.Image.MIMEType)
					measures[breakdownCatConversation].add(part.Image.Path)
				}
			}
		}
	} else if message.Content != "" {
		measureRuntimeText(message.Content, measures, measuredStaticCategories)
	}

	if message.ReasoningContent != "" {
		measures[breakdownCatConversation].add(message.ReasoningContent)
	}

	if len(message.ToolCalls) > 0 {
		measures[breakdownCatConversation].addOverhead(int64(len(message.ToolCalls)) * estimatedTokensPerToolCallOverhead)
		for _, toolCall := range message.ToolCalls {
			measures[breakdownCatConversation].add(toolCall.ID)
			measures[breakdownCatConversation].add(toolCall.Type)
			measures[breakdownCatConversation].add(toolCall.Function.Name)
			measures[breakdownCatConversation].add(toolCall.Function.Arguments)
		}
	}
}

func isMcpToolJSON(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, `"mcp_`) ||
		strings.Contains(lower, `"call_mcp_tool"`) ||
		strings.Contains(lower, `mcp-server`) ||
		strings.Contains(lower, `mcp_registry`) ||
		strings.Contains(lower, `"read_mcp_resource"`) ||
		strings.Contains(lower, `"list_mcp_resources"`)
}

func fitSpecialEstimates(estimates *[breakdownCatCount]int64, total int64) {
	specialTotal := int64(0)
	for i := 0; i < breakdownCatConversation; i++ {
		specialTotal += estimates[i]
	}
	if specialTotal <= total || specialTotal == 0 {
		return
	}
	original := *estimates
	assigned := int64(0)
	for i := 0; i < breakdownCatConversation; i++ {
		estimates[i] = (original[i] * total) / specialTotal
		assigned += estimates[i]
	}
	remainder := total - assigned

	type remOrder struct {
		index    int
		fraction int64
	}
	orders := make([]remOrder, breakdownCatConversation)
	for i := 0; i < breakdownCatConversation; i++ {
		orders[i] = remOrder{
			index:    i,
			fraction: (original[i] * total) % specialTotal,
		}
	}
	sort.Slice(orders, func(i, j int) bool {
		return orders[i].fraction > orders[j].fraction
	})
	for _, o := range orders {
		if remainder <= 0 {
			break
		}
		estimates[o.index]++
		remainder--
	}
}

func estimateCheckpointPromptTokenBreakdown(compiled CompiledConversation, hasCompiled bool, usedTokens uint32, maxTokens uint32) *agentv1.PromptTokenBreakdownSnapshot {
	if maxTokens == 0 {
		return nil
	}

	var measures [breakdownCatCount]breakdownMeasure
	var measuredStaticCategories [breakdownCatCount]bool

	if hasCompiled {
		for _, toolRaw := range compiled.Tools {
			s := string(toolRaw)
			if isMcpToolJSON(s) {
				measures[breakdownCatMCP].add(s)
			} else {
				measures[breakdownCatTools].add(s)
			}
		}

		for i := len(compiled.Messages) - 1; i >= 0; i-- {
			measureBreakdownMessage(compiled.Messages[i], &measures, &measuredStaticCategories)
		}
	}

	var estimates [breakdownCatCount]int64
	for i := 0; i < breakdownCatCount; i++ {
		estimates[i] = measures[i].estimatedTokens()
	}

	localTotal := int64(0)
	for _, est := range estimates {
		localTotal += est
	}

	effectiveUsedTokens := int64(usedTokens)
	if effectiveUsedTokens <= 0 {
		effectiveUsedTokens = localTotal
	}

	if effectiveUsedTokens > 0 {
		fitSpecialEstimates(&estimates, effectiveUsedTokens)
		specialSum := int64(0)
		for i := 0; i < breakdownCatConversation; i++ {
			specialSum += estimates[i]
		}
		if effectiveUsedTokens >= specialSum {
			estimates[breakdownCatConversation] = effectiveUsedTokens - specialSum
		} else {
			estimates[breakdownCatConversation] = 0
		}
	}

	categories := make([]*agentv1.PromptTokenBreakdownCategory, 0, breakdownCatCount+1)
	for i, def := range breakdownCategoryDefinitions {
		tokenVal := estimates[i]
		if tokenVal <= 0 && def.id != "conversation" && def.id != "system_prompt" && def.id != "tools" {
			if measures[i].characters == 0 && measures[i].overheadTokens == 0 {
				continue
			}
		}
		var charCount *uint32
		if measures[i].characters > 0 {
			c := clampInt64ToUint32(measures[i].characters)
			charCount = &c
		}
		categories = append(categories, &agentv1.PromptTokenBreakdownCategory{
			Id:              def.id,
			Label:           def.label,
			EstimatedTokens: clampInt64ToUint32(tokenVal),
			CharacterCount:  charCount,
		})
	}

	finalUsedTokens := effectiveUsedTokens
	categorySum := int64(0)
	for _, c := range categories {
		categorySum += int64(c.GetEstimatedTokens())
	}
	if categorySum > finalUsedTokens {
		finalUsedTokens = categorySum
	}
	if finalUsedTokens <= 0 {
		finalUsedTokens = 1
	}

	return &agentv1.PromptTokenBreakdownSnapshot{
		TotalUsedTokens: clampInt64ToUint32(finalUsedTokens),
		MaxTokens:       maxTokens,
		Categories:      categories,
	}
}

func clampInt64ToInt32(value int64) int32 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}
