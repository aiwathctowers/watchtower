package targets

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"watchtower/internal/db"
)

// LinkResult holds the AI-proposed parent and secondary links for an existing target.
type LinkResult struct {
	ParentID       sql.NullInt64
	SecondaryLinks []ProposedLink
}

// aiLinkResponse is the raw JSON for the link prompt response.
type aiLinkResponse struct {
	ParentID       *int64            `json:"parent_id"`
	SecondaryLinks []aiSecondaryLink `json:"secondary_links"`
}

// buildLinkPrompt assembles the smaller prompt for single-target linking.
func buildLinkPrompt(target db.Target, snapshot []db.Target) string {
	intent := ""
	if target.Intent != "" {
		intent = "Intent: " + target.Intent
	}

	snapshotBlock := ""
	if len(snapshot) > 0 {
		var sb strings.Builder
		sb.WriteString("=== ACTIVE TARGETS ===\n")
		for _, t := range snapshot {
			if t.ID == target.ID {
				continue // skip self
			}
			sb.WriteString(fmt.Sprintf("[id=%d level=%s period=%s..%s priority=%s status=%s] %s\n",
				t.ID, t.Level, t.PeriodStart, t.PeriodEnd, t.Priority, t.Status, t.Text))
		}
		sb.WriteString("=== /ACTIVE TARGETS ===")
		snapshotBlock = sb.String()
	}

	return fmt.Sprintf(LinkPromptTemplate,
		target.ID, target.Level, target.PeriodStart, target.PeriodEnd,
		target.Priority, target.Status, target.Text,
		intent,
		snapshotBlock,
	)
}

// parseLinkResponse parses the AI link response, validates ids against the snapshot.
func parseLinkResponse(raw string, snapshot []db.Target) (*LinkResult, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown fences.
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

	var resp aiLinkResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parsing AI link response: %w", err)
	}

	// Build snapshot id set.
	snapshotIDs := make(map[int64]bool, len(snapshot))
	for _, t := range snapshot {
		snapshotIDs[int64(t.ID)] = true
	}

	result := &LinkResult{}

	// Validate parent_id.
	if resp.ParentID != nil && snapshotIDs[*resp.ParentID] {
		result.ParentID = sql.NullInt64{Int64: *resp.ParentID, Valid: true}
	}

	// Cap secondary links at 3.
	links := resp.SecondaryLinks
	if len(links) > 3 {
		links = links[:3]
	}

	validRelations := map[string]bool{
		"contributes_to": true,
		"blocks":         true,
		"related":        true,
		"duplicates":     true,
	}

	for _, sl := range links {
		if !validRelations[sl.Relation] {
			continue
		}
		// Validate external_ref allowlist.
		if sl.ExternalRef != "" && !IsValidExternalRef(sl.ExternalRef) {
			continue // drop invalid external refs silently in AI path
		}
		pl := ProposedLink{
			ExternalRef: sl.ExternalRef,
			Relation:    sl.Relation,
		}
		if sl.Confidence > 0 {
			pl.Confidence = sql.NullFloat64{Float64: sl.Confidence, Valid: true}
		}
		if sl.TargetID != nil {
			if !snapshotIDs[*sl.TargetID] {
				continue // drop unknown id
			}
			pl.TargetID = sql.NullInt64{Int64: *sl.TargetID, Valid: true}
		}
		if !pl.TargetID.Valid && pl.ExternalRef == "" {
			continue
		}
		result.SecondaryLinks = append(result.SecondaryLinks, pl)
	}

	return result, nil
}
