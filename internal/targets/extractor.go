package targets

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"watchtower/internal/db"
)

// ProposedTarget holds one AI-extracted target before DB persistence.
type ProposedTarget struct {
	Text              string
	Intent            string
	Level             string
	CustomLabel       string
	PeriodStart       string
	PeriodEnd         string
	Priority          string
	DueDate           string
	ParentID          sql.NullInt64
	SecondaryLinks    []ProposedLink
	SubItems          []ProposedSubItem
	AILevelConfidence sql.NullFloat64
}

// ProposedSubItem is one step/deliverable under a single extracted target.
// The AI returns just the text; done is always false on creation.
type ProposedSubItem struct {
	Text string
}

// ProposedLink is a secondary link proposal from the AI.
type ProposedLink struct {
	TargetID    sql.NullInt64
	ExternalRef string
	Relation    string
	Confidence  sql.NullFloat64
}

// ExtractRequest carries the inputs for an extraction AI call.
type ExtractRequest struct {
	RawText    string // paste / message body / form input
	EntryPoint string // 'create_sheet' | 'inbox' | 'chat' | 'cli'
	SourceRef  string // 'inbox:42' | 'slack:C123:...' | '' for manual paste
	UserLevel  string // optional hint; if set, AI prefers this level
}

// ExtractResult holds the parsed AI response for extraction.
type ExtractResult struct {
	Extracted    []ProposedTarget
	OmittedCount int
	Notes        string
}

// aiExtractResponse is the raw JSON schema from the AI.
type aiExtractResponse struct {
	Extracted    []aiExtractedItem `json:"extracted"`
	OmittedCount int               `json:"omitted_count"`
	Notes        string            `json:"notes"`
}

type aiExtractedItem struct {
	Text            string            `json:"text"`
	Intent          string            `json:"intent"`
	Level           string            `json:"level"`
	CustomLabel     string            `json:"custom_label"`
	LevelConfidence float64           `json:"level_confidence"`
	PeriodStart     string            `json:"period_start"`
	PeriodEnd       string            `json:"period_end"`
	Priority        string            `json:"priority"`
	DueDate         string            `json:"due_date"`
	ParentID        *int64            `json:"parent_id"`
	SecondaryLinks  []aiSecondaryLink `json:"secondary_links"`
	SubItems        []aiSubItem       `json:"sub_items"`
}

type aiSubItem struct {
	Text string `json:"text"`
}

type aiSecondaryLink struct {
	TargetID    *int64  `json:"target_id"`
	ExternalRef string  `json:"external_ref"`
	Relation    string  `json:"relation"`
	Confidence  float64 `json:"confidence"`
}

// buildExtractPrompt assembles the full extraction prompt per spec.
func buildExtractPrompt(req ExtractRequest, enrichments []Enrichment, activeSnapshot []db.Target, now time.Time) string {
	// ENRICHMENTS block.
	enrichBlock := ""
	if len(enrichments) > 0 {
		var sb strings.Builder
		sb.WriteString("=== ENRICHMENTS ===\n")
		for _, e := range enrichments {
			sb.WriteString(fmt.Sprintf("[%s]\n", e.Ref))
			if e.Error != "" {
				sb.WriteString(fmt.Sprintf("%s\n", e.Body))
			} else if e.Body != "" {
				sb.WriteString(e.Body)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("=== /ENRICHMENTS ===")
		enrichBlock = sb.String()
	}

	// ACTIVE TARGETS block.
	snapshotBlock := ""
	if len(activeSnapshot) > 0 {
		var sb strings.Builder
		sb.WriteString("=== ACTIVE TARGETS ===\n")
		for _, t := range activeSnapshot {
			parentStr := ""
			if t.ParentID.Valid {
				parentStr = fmt.Sprintf(" parent=%d", t.ParentID.Int64)
			}
			sb.WriteString(fmt.Sprintf("[id=%d level=%s period=%s..%s priority=%s status=%s%s] %s\n",
				t.ID, t.Level, t.PeriodStart, t.PeriodEnd, t.Priority, t.Status, parentStr, t.Text))
		}
		sb.WriteString("=== /ACTIVE TARGETS ===")
		snapshotBlock = sb.String()
	}

	// USER HINT block.
	hintBlock := ""
	if req.UserLevel != "" {
		hintBlock = fmt.Sprintf("=== USER HINT ===\nPrefer level=%s for extracted targets.\n=== /USER HINT ===", req.UserLevel)
	}

	return fmt.Sprintf(ExtractPromptTemplate,
		req.RawText,
		enrichBlock,
		snapshotBlock,
		now.Format("2006-01-02"),
		hintBlock,
	)
}

// IsValidExternalRef reports whether ref has an allowed prefix ("jira:" or "slack:").
func IsValidExternalRef(ref string) bool {
	return strings.HasPrefix(ref, "jira:") || strings.HasPrefix(ref, "slack:")
}

// parseExtractResponse parses the JSON from the AI, enforces caps, validates ids.
// On malformed JSON it returns an error (caller retries once).
func parseExtractResponse(raw string, activeSnapshot []db.Target, logger *log.Logger) (*ExtractResult, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown fences if AI wrapped the response.
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			raw = lines[1]
		}
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}

	var resp aiExtractResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parsing AI extract response: %w", err)
	}

	// Build snapshot id set for validation.
	snapshotIDs := make(map[int64]bool, len(activeSnapshot))
	for _, t := range activeSnapshot {
		snapshotIDs[int64(t.ID)] = true
	}

	// Cap extracted to 10.
	omitted := resp.OmittedCount
	if len(resp.Extracted) > 10 {
		extra := len(resp.Extracted) - 10
		omitted += extra
		resp.Extracted = resp.Extracted[:10]
		if logger != nil {
			logger.Printf("targets/extractor: AI returned %d items, truncated to 10 (omitted %d)", len(resp.Extracted)+extra, extra)
		}
	}

	validRelations := map[string]bool{
		"contributes_to": true,
		"blocks":         true,
		"related":        true,
		"duplicates":     true,
	}

	var result []ProposedTarget
	for _, item := range resp.Extracted {
		pt := ProposedTarget{
			Text:        item.Text,
			Intent:      item.Intent,
			Level:       item.Level,
			CustomLabel: item.CustomLabel,
			PeriodStart: item.PeriodStart,
			PeriodEnd:   item.PeriodEnd,
			Priority:    item.Priority,
			DueDate:     item.DueDate,
		}

		// Validate and apply level_confidence.
		if item.LevelConfidence > 0 {
			pt.AILevelConfidence = sql.NullFloat64{Float64: item.LevelConfidence, Valid: true}
		}

		// Validate parent_id.
		if item.ParentID != nil {
			if snapshotIDs[*item.ParentID] {
				pt.ParentID = sql.NullInt64{Int64: *item.ParentID, Valid: true}
			} else if logger != nil {
				logger.Printf("targets/extractor: unknown parent_id %d — setting NULL", *item.ParentID)
			}
		}

		// Swap period dates if in wrong order.
		if pt.PeriodStart != "" && pt.PeriodEnd != "" && pt.PeriodEnd < pt.PeriodStart {
			pt.PeriodStart, pt.PeriodEnd = pt.PeriodEnd, pt.PeriodStart
		}

		// Cap secondary links at 3.
		links := item.SecondaryLinks
		if len(links) > 3 {
			if logger != nil {
				logger.Printf("targets/extractor: target %q has %d secondary links, truncating to 3", item.Text, len(links))
			}
			links = links[:3]
		}

		for _, sl := range links {
			// Validate relation.
			if !validRelations[sl.Relation] {
				if logger != nil {
					logger.Printf("targets/extractor: unknown relation %q — dropping link", sl.Relation)
				}
				continue
			}

			// Validate external_ref allowlist before building the link.
			if sl.ExternalRef != "" && !IsValidExternalRef(sl.ExternalRef) {
				if logger != nil {
					logger.Printf("targets/extractor: invalid external_ref %q (must start with jira: or slack:) — dropping link", sl.ExternalRef)
				}
				continue
			}

			pl := ProposedLink{
				ExternalRef: sl.ExternalRef,
				Relation:    sl.Relation,
			}
			if sl.Confidence > 0 {
				pl.Confidence = sql.NullFloat64{Float64: sl.Confidence, Valid: true}
			}

			// Validate target_id if set.
			if sl.TargetID != nil {
				if snapshotIDs[*sl.TargetID] {
					pl.TargetID = sql.NullInt64{Int64: *sl.TargetID, Valid: true}
				} else {
					if logger != nil {
						logger.Printf("targets/extractor: unknown secondary target_id %d — dropping link", *sl.TargetID)
					}
					continue
				}
			}

			// Must have either target_id or external_ref.
			if !pl.TargetID.Valid && pl.ExternalRef == "" {
				continue
			}

			pt.SecondaryLinks = append(pt.SecondaryLinks, pl)
		}

		// Cap sub_items at 15 to prevent runaway sub-lists.
		subs := item.SubItems
		if len(subs) > 15 {
			if logger != nil {
				logger.Printf("targets/extractor: target %q has %d sub_items, truncating to 15", item.Text, len(subs))
			}
			subs = subs[:15]
		}
		for _, s := range subs {
			text := strings.TrimSpace(s.Text)
			if text == "" {
				continue
			}
			pt.SubItems = append(pt.SubItems, ProposedSubItem{Text: text})
		}

		result = append(result, pt)
	}

	return &ExtractResult{
		Extracted:    result,
		OmittedCount: omitted,
		Notes:        resp.Notes,
	}, nil
}
