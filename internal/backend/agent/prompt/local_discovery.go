package promptengine

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"cursor/gen/agentv1"
)

func getCandidateRoots(requestContext *agentv1.RequestContext) []string {
	seen := make(map[string]struct{})
	var roots []string

	addRoot := func(p string) {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return
		}
		clean := filepath.Clean(trimmed)
		key := strings.ToLower(filepath.ToSlash(clean))
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			roots = append(roots, clean)
		}
	}

	if requestContext != nil {
		if env := requestContext.GetEnv(); env != nil {
			for _, wp := range env.GetWorkspacePaths() {
				addRoot(wp)
			}
			addRoot(env.GetProjectFolder())
		}
		for _, repo := range requestContext.GetGitRepos() {
			if repo != nil {
				addRoot(repo.GetPath())
			}
		}
	}

	if len(roots) == 0 {
		if wd, err := os.Getwd(); err == nil && wd != "" {
			addRoot(wd)
		}
	}

	return roots
}

func parseFrontmatter(content string) (map[string]interface{}, string) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return nil, trimmed
	}
	rest := trimmed[3:]
	idx := strings.Index(rest, "---")
	if idx == -1 {
		return nil, trimmed
	}
	yamlBlock := rest[:idx]
	body := strings.TrimSpace(rest[idx+3:])

	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &raw); err != nil {
		return nil, body
	}
	return raw, body
}

func extractFallbackDescription(body string, fallback string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimLeft(line, "# ")
		if line != "" {
			return line
		}
	}
	return fallback
}

// mergeLocalSkills 自动发现工作区及用户主目录下的 skills 目录，合并至 requestContext。
func mergeLocalSkills(requestContext *agentv1.RequestContext) {
	if requestContext == nil {
		return
	}

	knownPaths := make(map[string]struct{})
	knownNames := make(map[string]struct{})

	for _, s := range requestContext.GetAgentSkills() {
		if s == nil {
			continue
		}
		pathKey := strings.ToLower(filepath.ToSlash(filepath.Clean(s.GetFullPath())))
		if pathKey != "" {
			knownPaths[pathKey] = struct{}{}
		}
	}

	roots := getCandidateRoots(requestContext)
	var skillDirs []string
	for _, root := range roots {
		skillDirs = append(skillDirs,
			filepath.Join(root, ".cursor", "skills"),
			filepath.Join(root, ".agents", "skills"),
			filepath.Join(root, ".claude", "skills"),
		)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		skillDirs = append(skillDirs,
			filepath.Join(home, ".cursor", "skills"),
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".claude", "skills"),
		)
	}

	for _, dir := range skillDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				for _, cand := range []string{"SKILL.md", "skill.md", "Skill.md"} {
					skillFile := filepath.Join(entryPath, cand)
					if info, err := os.Stat(skillFile); err == nil && !info.IsDir() {
						addSkillFromFile(requestContext, skillFile, entry.Name(), knownPaths, knownNames)
						break
					}
				}
			} else if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				addSkillFromFile(requestContext, entryPath, stem, knownPaths, knownNames)
			}
		}
	}
}

func addSkillFromFile(requestContext *agentv1.RequestContext, filePath string, defaultName string, knownPaths map[string]struct{}, knownNames map[string]struct{}) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	pathKey := strings.ToLower(filepath.ToSlash(filepath.Clean(absPath)))
	if _, ok := knownPaths[pathKey]; ok {
		return
	}

	contentBytes, err := os.ReadFile(absPath)
	if err != nil || len(bytes.TrimSpace(contentBytes)) == 0 {
		return
	}
	content := string(contentBytes)
	fm, body := parseFrontmatter(content)

	name := defaultName
	description := ""
	disableModel := false

	if fm != nil {
		if n, ok := fm["name"].(string); ok && strings.TrimSpace(n) != "" {
			name = strings.TrimSpace(n)
		}
		if d, ok := fm["description"].(string); ok && strings.TrimSpace(d) != "" {
			description = strings.TrimSpace(d)
		}
		if dis, ok := fm["disable_model_invocation"].(bool); ok {
			disableModel = dis
		}
	}

	if disableModel {
		return
	}

	nameKey := strings.ToLower(name)
	if _, ok := knownNames[nameKey]; ok && nameKey != "" {
		return
	}

	if description == "" {
		description = extractFallbackDescription(body, name)
	}

	knownPaths[pathKey] = struct{}{}
	if nameKey != "" {
		knownNames[nameKey] = struct{}{}
	}

	requestContext.AgentSkills = append(requestContext.AgentSkills, &agentv1.AgentSkill{
		FullPath:               absPath,
		Content:                content,
		Description:            description,
		DisableModelInvocation: false,
	})
}

// mergeLocalSubagents 自动发现工作区及用户主目录下的 agents/subagents 目录，合并至 requestContext。
func mergeLocalSubagents(requestContext *agentv1.RequestContext) {
	if requestContext == nil {
		return
	}

	knownNames := make(map[string]struct{})
	for _, agent := range requestContext.GetCustomSubagents() {
		if agent == nil {
			continue
		}
		nameKey := strings.ToLower(strings.TrimSpace(agent.GetName()))
		if nameKey != "" {
			knownNames[nameKey] = struct{}{}
		}
	}

	roots := getCandidateRoots(requestContext)
	var agentDirs []string
	for _, root := range roots {
		agentDirs = append(agentDirs,
			filepath.Join(root, ".cursor", "agents"),
			filepath.Join(root, ".cursor", "subagents"),
			filepath.Join(root, ".agents", "subagents"),
			filepath.Join(root, ".claude", "agents"),
		)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		agentDirs = append(agentDirs,
			filepath.Join(home, ".cursor", "agents"),
			filepath.Join(home, ".cursor", "subagents"),
			filepath.Join(home, ".agents", "subagents"),
		)
	}

	for _, dir := range agentDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				continue
			}
			filePath := filepath.Join(dir, entry.Name())
			absPath, err := filepath.Abs(filePath)
			if err != nil {
				absPath = filePath
			}
			stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			addAgentFromFile(requestContext, absPath, stem, knownNames)
		}
	}
}

func addAgentFromFile(requestContext *agentv1.RequestContext, absPath string, defaultName string, knownNames map[string]struct{}) {
	contentBytes, err := os.ReadFile(absPath)
	if err != nil || len(bytes.TrimSpace(contentBytes)) == 0 {
		return
	}
	content := string(contentBytes)
	fm, body := parseFrontmatter(content)

	name := defaultName
	description := ""
	model := ""

	if fm != nil {
		if n, ok := fm["name"].(string); ok && strings.TrimSpace(n) != "" {
			name = strings.TrimSpace(n)
		}
		if d, ok := fm["description"].(string); ok && strings.TrimSpace(d) != "" {
			description = strings.TrimSpace(d)
		}
		if m, ok := fm["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}

	nameKey := strings.ToLower(name)
	if _, ok := knownNames[nameKey]; ok && nameKey != "" {
		return
	}

	if description == "" {
		description = extractFallbackDescription(body, name)
	}

	if nameKey != "" {
		knownNames[nameKey] = struct{}{}
	}

	requestContext.CustomSubagents = append(requestContext.CustomSubagents, &agentv1.CustomSubagent{
		FullPath:    absPath,
		Name:        name,
		Description: description,
		Model:       model,
		Prompt:      body,
	})
}