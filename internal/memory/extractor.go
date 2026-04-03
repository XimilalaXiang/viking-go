package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/vikingfs"
	"github.com/google/uuid"
)

// MemoryCategory represents the 8 types of extractable memories.
type MemoryCategory string

const (
	CatProfile     MemoryCategory = "profile"
	CatPreferences MemoryCategory = "preferences"
	CatEntities    MemoryCategory = "entities"
	CatEvents      MemoryCategory = "events"
	CatCases       MemoryCategory = "cases"
	CatPatterns    MemoryCategory = "patterns"
	CatTools       MemoryCategory = "tools"
	CatSkills      MemoryCategory = "skills"
)

var validCategories = map[string]MemoryCategory{
	"profile":     CatProfile,
	"preferences": CatPreferences,
	"entities":    CatEntities,
	"events":      CatEvents,
	"cases":       CatCases,
	"patterns":    CatPatterns,
	"tools":       CatTools,
	"skills":      CatSkills,
}

var userCategories = map[MemoryCategory]bool{
	CatProfile:     true,
	CatPreferences: true,
	CatEntities:    true,
	CatEvents:      true,
}

var categoryDirs = map[MemoryCategory]string{
	CatProfile:     "memories/profile.md",
	CatPreferences: "memories/preferences",
	CatEntities:    "memories/entities",
	CatEvents:      "memories/events",
	CatCases:       "memories/cases",
	CatPatterns:    "memories/patterns",
	CatTools:       "memories/tools",
	CatSkills:      "memories/skills",
}

// CandidateMemory is a memory extracted from session messages.
type CandidateMemory struct {
	Category      MemoryCategory `json:"category"`
	Abstract      string         `json:"abstract"`
	Overview      string         `json:"overview"`
	Content       string         `json:"content"`
	SourceSession string         `json:"source_session"`
	User          string         `json:"user"`
	Language      string         `json:"language"`
}

// MergedPayload is the result of merging two memory bundles via LLM.
type MergedPayload struct {
	Abstract string `json:"abstract"`
	Overview string `json:"overview"`
	Content  string `json:"content"`
	Reason   string `json:"reason"`
}

// Extractor extracts memories from session messages using LLM.
type Extractor struct {
	llm llm.Client
	vfs *vikingfs.VikingFS
}

// NewExtractor creates a new memory extractor.
func NewExtractor(llmClient llm.Client, vfs *vikingfs.VikingFS) *Extractor {
	return &Extractor{llm: llmClient, vfs: vfs}
}

// Extract calls LLM to extract candidate memories from formatted session messages.
func (e *Extractor) Extract(
	messages []SessionMessage,
	sessionID string,
	user string,
	historySummary string,
) ([]CandidateMemory, error) {
	if e.llm == nil {
		return nil, nil
	}

	formatted := formatSessionMessages(messages)
	if formatted == "" {
		return nil, nil
	}

	lang := detectLanguage(messages)

	prompt := buildExtractionPrompt(historySummary, formatted, user, lang)

	resp, err := e.llm.CompleteWithPrompt(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM extraction: %w", err)
	}

	data, err := ParseJSONFromResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse extraction response: %w", err)
	}

	return parseCandidates(data, sessionID, user, lang), nil
}

// CreateMemory persists a candidate memory to VikingFS and returns a Context.
func (e *Extractor) CreateMemory(
	candidate CandidateMemory,
	sessionID string,
	reqCtx *ctx.RequestContext,
) (*ctx.Context, error) {
	if e.vfs == nil {
		return nil, fmt.Errorf("VikingFS not available")
	}

	ownerSpace := getOwnerSpace(candidate.Category, reqCtx)

	if candidate.Category == CatProfile {
		return e.handleProfile(candidate, sessionID, reqCtx, ownerSpace)
	}

	catDir := categoryDirs[candidate.Category]
	var parentURI string
	if userCategories[candidate.Category] {
		parentURI = fmt.Sprintf("viking://user/%s/%s", reqCtx.User.UserSpaceName(), catDir)
	} else {
		parentURI = fmt.Sprintf("viking://agent/%s/%s", reqCtx.User.AgentSpaceName(), catDir)
	}

	memID := "mem_" + uuid.New().String()
	memURI := parentURI + "/" + memID + ".md"

	if err := e.vfs.WriteString(memURI, candidate.Content, reqCtx); err != nil {
		return nil, fmt.Errorf("write memory: %w", err)
	}
	log.Printf("Created memory file: %s", memURI)

	memory := ctx.NewContext(memURI,
		ctx.WithParentURI(parentURI),
		ctx.WithIsLeaf(true),
		ctx.WithAbstract(candidate.Abstract),
		ctx.WithContextType(string(ctx.TypeMemory)),
		ctx.WithCategory(string(candidate.Category)),
		ctx.WithSessionID(sessionID),
		ctx.WithAccountID(reqCtx.AccountID),
		ctx.WithOwnerSpace(ownerSpace),
	)
	memory.VectorizeText = candidate.Content

	return memory, nil
}

func (e *Extractor) handleProfile(
	candidate CandidateMemory,
	sessionID string,
	reqCtx *ctx.RequestContext,
	ownerSpace string,
) (*ctx.Context, error) {
	userSpace := reqCtx.User.UserSpaceName()
	profileURI := fmt.Sprintf("viking://user/%s/memories/profile.md", userSpace)

	existing, _ := e.vfs.ReadFile(profileURI, reqCtx)

	vectorizeText := candidate.Content

	if strings.TrimSpace(existing) != "" && e.llm != nil {
		payload, err := e.mergeMemoryBundle("", "", existing, candidate.Abstract, candidate.Overview, candidate.Content, "profile", candidate.Language)
		if err == nil && payload != nil {
			if err := e.vfs.WriteString(profileURI, payload.Content, reqCtx); err != nil {
				return nil, fmt.Errorf("write merged profile: %w", err)
			}
			vectorizeText = payload.Content
		}
	} else {
		if err := e.vfs.WriteString(profileURI, candidate.Content, reqCtx); err != nil {
			return nil, fmt.Errorf("write profile: %w", err)
		}
	}

	memory := ctx.NewContext(profileURI,
		ctx.WithParentURI(fmt.Sprintf("viking://user/%s/memories", userSpace)),
		ctx.WithIsLeaf(true),
		ctx.WithAbstract(candidate.Abstract),
		ctx.WithContextType(string(ctx.TypeMemory)),
		ctx.WithCategory(string(CatProfile)),
		ctx.WithSessionID(sessionID),
		ctx.WithAccountID(reqCtx.AccountID),
		ctx.WithOwnerSpace(ownerSpace),
	)
	memory.VectorizeText = vectorizeText

	return memory, nil
}

// MergeMemoryBundle uses LLM to merge existing and new memory bundles.
func (e *Extractor) mergeMemoryBundle(
	existAbstract, existOverview, existContent,
	newAbstract, newOverview, newContent,
	category, language string,
) (*MergedPayload, error) {
	if e.llm == nil {
		return nil, fmt.Errorf("LLM not available")
	}

	prompt := buildMergePrompt(existAbstract, existOverview, existContent,
		newAbstract, newOverview, newContent, category, language)

	resp, err := e.llm.CompleteWithPrompt(prompt)
	if err != nil {
		return nil, err
	}

	data, err := ParseJSONFromResponse(resp.Content)
	if err != nil {
		return nil, err
	}

	abstract := strings.TrimSpace(getStr(data, "abstract"))
	content := strings.TrimSpace(getStr(data, "content"))
	if abstract == "" || content == "" {
		return nil, fmt.Errorf("merged payload missing abstract/content")
	}

	decision := strings.ToLower(strings.TrimSpace(getStr(data, "decision")))
	if decision != "" && decision != "merge" {
		return nil, fmt.Errorf("invalid merge decision: %s", decision)
	}

	return &MergedPayload{
		Abstract: abstract,
		Overview: strings.TrimSpace(getStr(data, "overview")),
		Content:  content,
		Reason:   strings.TrimSpace(getStr(data, "reason")),
	}, nil
}

// SessionMessage represents a session message for extraction.
type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func formatSessionMessages(messages []SessionMessage) string {
	var lines []string
	for _, m := range messages {
		if m.Content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", m.Role, m.Content))
	}
	return strings.Join(lines, "\n")
}

var (
	reKo   = regexp.MustCompile(`[\x{AC00}-\x{D7AF}]`)
	reRu   = regexp.MustCompile(`[\x{0400}-\x{04FF}]`)
	reAr   = regexp.MustCompile(`[\x{0600}-\x{06FF}]`)
	reKana = regexp.MustCompile(`[\x{3040}-\x{30FF}\x{31F0}-\x{31FF}\x{FF66}-\x{FF9F}]`)
)

func detectLanguage(messages []SessionMessage) string {
	var userText strings.Builder
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			userText.WriteString(m.Content)
			userText.WriteString("\n")
		}
	}
	text := userText.String()
	if text == "" {
		return "en"
	}

	totalChars := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			totalChars++
		}
	}
	if totalChars == 0 {
		return "en"
	}

	koCount := len(reKo.FindAllString(text, -1))
	ruCount := len(reRu.FindAllString(text, -1))
	arCount := len(reAr.FindAllString(text, -1))

	best, bestCount := "", 0
	for _, pair := range []struct {
		lang  string
		count int
	}{
		{"ko", koCount},
		{"ru", ruCount},
		{"ar", arCount},
	} {
		if pair.count > bestCount {
			best = pair.lang
			bestCount = pair.count
		}
	}
	if bestCount >= 2 && float64(bestCount)/float64(totalChars) >= 0.10 {
		return best
	}

	kanaCount := len(reKana.FindAllString(text, -1))
	hanCount := 0
	for _, r := range text {
		if r >= '\u4E00' && r <= '\u9FFF' {
			hanCount++
		}
	}
	if kanaCount > 0 {
		return "ja"
	}
	if hanCount > 0 {
		return "zh-CN"
	}

	return "en"
}

func buildExtractionPrompt(summary, messages, user, language string) string {
	return fmt.Sprintf(`You are a memory extraction engine. Analyze the conversation and extract important memories.

Session Summary: %s

Recent Messages:
%s

User: %s

Output language: %s

Extract memories in these categories:
- profile: User personal information
- preferences: User preferences (tools, styles, languages)
- entities: Important entities (projects, people, concepts)
- events: Significant events (decisions, milestones)
- cases: Problem-solution cases
- patterns: Reusable processes/methods

Return JSON:
{"memories": [{"category": "...", "abstract": "one-line summary", "overview": "medium detail", "content": "full narrative"}]}

Only extract genuinely important, reusable information. Return empty memories array if nothing worth remembering.`, summary, messages, user, language)
}

func buildMergePrompt(existAbstract, existOverview, existContent, newAbstract, newOverview, newContent, category, language string) string {
	return fmt.Sprintf(`Merge these two memory entries into one cohesive entry.

Existing:
- Abstract: %s
- Overview: %s
- Content: %s

New:
- Abstract: %s
- Overview: %s
- Content: %s

Category: %s
Output language: %s

Return JSON: {"decision": "merge", "abstract": "...", "overview": "...", "content": "...", "reason": "..."}`, existAbstract, existOverview, existContent, newAbstract, newOverview, newContent, category, language)
}

func parseCandidates(data map[string]any, sessionID, user, lang string) []CandidateMemory {
	memoriesRaw, ok := data["memories"]
	if !ok {
		return nil
	}
	memories, ok := memoriesRaw.([]any)
	if !ok {
		return nil
	}

	var candidates []CandidateMemory
	for _, memRaw := range memories {
		mem, ok := memRaw.(map[string]any)
		if !ok {
			continue
		}

		catStr := getStr(mem, "category")
		cat, ok := validCategories[catStr]
		if !ok {
			cat = CatPatterns
		}

		abstract := getStr(mem, "abstract")
		content := getStr(mem, "content")
		if abstract == "" && content == "" {
			continue
		}

		candidates = append(candidates, CandidateMemory{
			Category:      cat,
			Abstract:      abstract,
			Overview:      getStr(mem, "overview"),
			Content:       content,
			SourceSession: sessionID,
			User:          user,
			Language:       lang,
		})
	}
	return candidates
}

func getOwnerSpace(cat MemoryCategory, reqCtx *ctx.RequestContext) string {
	if userCategories[cat] {
		return reqCtx.User.UserSpaceName()
	}
	return reqCtx.User.AgentSpaceName()
}

// ParseJSONFromResponse extracts and parses a JSON object from LLM response text.
func ParseJSONFromResponse(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)

	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			lines = lines[1:]
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					lines = lines[:i]
					break
				}
			}
			text = strings.Join(lines, "\n")
		}
	}

	start := strings.Index(text, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found")
	}

	depth := 0
	end := -1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
		if end > 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unmatched JSON braces")
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(text[start:end]), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
