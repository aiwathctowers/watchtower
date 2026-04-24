package inbox

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"text/template"
	"time"

	"watchtower/internal/db"
	"watchtower/internal/digest"
)

//go:embed prompts/select_pinned.tmpl
var selectPinnedTmpl string

const maxPinned = 5

// PinnedSelector uses AI to select the most critical inbox items to pin.
type PinnedSelector struct {
	db     *db.DB
	gen    digest.Generator
	logger *log.Logger
}

// NewPinnedSelector creates a PinnedSelector with the given DB and generator.
func NewPinnedSelector(database *db.DB, gen digest.Generator) *PinnedSelector {
	return &PinnedSelector{db: database, gen: gen, logger: log.Default()}
}

// pinnedResp is the expected JSON response from the AI.
type pinnedResp struct {
	PinnedIDs []int64 `json:"pinned_ids"`
	Reason    string  `json:"reason"`
}

// Run calls the AI to select pinned items, filters muted items, caps at maxPinned,
// and persists the result. On AI error or invalid JSON it keeps the existing pinned
// state (non-fatal). Returns the number of pinned items set.
func (p *PinnedSelector) Run(ctx context.Context) (int, error) {
	items, err := p.db.ListActionableOpen()
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		_ = p.db.ClearPinnedAll()
		return 0, nil
	}

	prefs, err := buildUserPreferencesBlock(p.db, items)
	if err != nil {
		return 0, err
	}

	prompt, err := renderPinnedPrompt(items, prefs)
	if err != nil {
		return 0, err
	}

	// Use Generate with empty systemPrompt and sessionID (single-shot query).
	resp, _, _, err := p.gen.Generate(ctx, "", prompt, "")
	if err != nil {
		// Keep existing pinned state — do not clear.
		p.logger.Printf("pinned_selector: AI call failed (keeping state): %v", err)
		return 0, nil
	}

	parsed, err := parsePinnedResponse(resp)
	if err != nil {
		// Fallback: keep existing state.
		p.logger.Printf("pinned_selector: invalid JSON from AI (keeping state): %v", err)
		return 0, nil
	}

	// Filter out muted items even if the AI suggested them.
	mutes := loadMuteScopes(p.db)
	filtered := filterNotMuted(parsed.PinnedIDs, items, mutes)
	if len(filtered) > maxPinned {
		filtered = filtered[:maxPinned]
	}

	if err := p.db.SetInboxPinned(filtered); err != nil {
		return 0, err
	}
	return len(filtered), nil
}

func renderPinnedPrompt(items []db.InboxItem, prefs string) (string, error) {
	tmpl, err := template.New("select_pinned").Parse(selectPinnedTmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]interface{}{
		"Now":             time.Now().Format(time.RFC3339),
		"CalendarContext": "",
		"UserPreferences": prefs,
		"Items":           items,
		"MaxPinned":       maxPinned,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func parsePinnedResponse(s string) (pinnedResp, error) {
	var r pinnedResp
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, err
	}
	return r, nil
}

func loadMuteScopes(database *db.DB) map[string]bool {
	rules, _ := database.ListAllLearnedRules()
	m := map[string]bool{}
	for _, r := range rules {
		if r.RuleType == "source_mute" && r.Weight <= -0.8 {
			m[r.ScopeKey] = true
		}
	}
	return m
}

func filterNotMuted(ids []int64, items []db.InboxItem, mutes map[string]bool) []int64 {
	byID := map[int64]db.InboxItem{}
	for _, it := range items {
		byID[int64(it.ID)] = it
	}
	var out []int64
	for _, id := range ids {
		it, ok := byID[id]
		if !ok {
			continue
		}
		if mutes["sender:"+it.SenderUserID] {
			continue
		}
		if mutes["channel:"+it.ChannelID] {
			continue
		}
		out = append(out, id)
	}
	return out
}
