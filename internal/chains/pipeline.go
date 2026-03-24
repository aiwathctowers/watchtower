// Package chains links related decisions, digests, and tracks into thematic chains,
// reducing noise in daily/weekly rollups by grouping repeated topics.
package chains

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// DefaultStaleDays is how many days without activity before a chain becomes stale.
const DefaultStaleDays = 14

// ProgressFunc reports pipeline progress: done items out of total, with a status message.
type ProgressFunc func(done, total int, status string)

// Pipeline links unlinked decisions and digests from channel digests into thematic chains.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	gen         digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	// OnProgress is called to report progress during chain linking.
	OnProgress ProgressFunc
}

// New creates a new chains pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{
		db:     database,
		cfg:    cfg,
		gen:    gen,
		logger: logger,
	}
}

// SetOnProgress sets the progress callback.
func (p *Pipeline) SetOnProgress(fn digest.ProgressFunc) {
	p.OnProgress = ProgressFunc(fn)
}

// SetPromptStore sets an optional prompt store for loading customized prompts.
func (p *Pipeline) SetPromptStore(store *prompts.Store) {
	p.promptStore = store
}

// digestContext holds a digest summary for building prompts.
type digestContext struct {
	DigestID    int
	ChannelID   string
	ChannelName string
	Summary     string
	Topics      string
	PeriodTo    float64
}

// Run links unlinked decisions and digests from recent channel digests to chains.
// Returns the number of items linked.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	runStart := time.Now()

	// 1. Get unlinked decisions from recent channel digests (last 14 days).
	cutoff := float64(time.Now().AddDate(0, 0, -DefaultStaleDays).Unix())
	t0 := time.Now()
	unlinked, err := p.db.GetUnlinkedDecisions(cutoff)
	if err != nil {
		return 0, fmt.Errorf("getting unlinked decisions: %w", err)
	}
	p.logger.Printf("chains: found %d unlinked decision(s) [%s]", len(unlinked), time.Since(t0).Round(time.Millisecond))

	// 1b. Get unlinked digests for richer context.
	t0 = time.Now()
	unlinkedDigests, err := p.db.GetUnlinkedDigests(cutoff)
	if err != nil {
		p.logger.Printf("chains: failed to get unlinked digests (continuing without): %v", err)
	}
	p.logger.Printf("chains: found %d unlinked digest(s) [%s]", len(unlinkedDigests), time.Since(t0).Round(time.Millisecond))

	total := len(unlinked) + len(unlinkedDigests)
	if p.OnProgress != nil {
		p.OnProgress(0, total, fmt.Sprintf("Found %d decisions, %d digests to link", len(unlinked), len(unlinkedDigests)))
	}

	if len(unlinked) == 0 && len(unlinkedDigests) == 0 {
		// Mark stale chains and return.
		if n, err := p.db.MarkStaleChains(cutoff); err != nil {
			p.logger.Printf("chains: mark stale error: %v", err)
		} else if n > 0 {
			p.logger.Printf("chains: marked %d chain(s) as stale", n)
		}
		return 0, nil
	}

	// 2. Get active chains (including parent/child info).
	t0 = time.Now()
	activeChains, err := p.db.GetActiveChains(DefaultStaleDays)
	if err != nil {
		return 0, fmt.Errorf("getting active chains: %w", err)
	}
	p.logger.Printf("chains: loaded %d active chain(s) [%s]", len(activeChains), time.Since(t0).Round(time.Millisecond))

	// 3. Build digest context for richer grouping.
	t0 = time.Now()
	var digContexts []digestContext
	for _, d := range unlinkedDigests {
		chName, _ := p.db.ChannelNameByID(d.ChannelID)
		digContexts = append(digContexts, digestContext{
			DigestID:    d.ID,
			ChannelID:   d.ChannelID,
			ChannelName: chName,
			Summary:     d.Summary,
			Topics:      d.Topics,
			PeriodTo:    d.PeriodTo,
		})
	}
	p.logger.Printf("chains: built digest contexts [%s]", time.Since(t0).Round(time.Millisecond))

	// 4. Call AI to link decisions to chains.
	if p.OnProgress != nil {
		p.OnProgress(0, total, fmt.Sprintf("Linking %d items (%d active chains)...", total, len(activeChains)))
	}
	t0 = time.Now()
	prompt := p.buildPrompt(activeChains, unlinked, digContexts)
	p.logger.Printf("chains: built prompt (%d bytes) [%s]", len(prompt), time.Since(t0).Round(time.Millisecond))

	systemPrompt := chainsSystemPrompt

	t0 = time.Now()
	resp, usage, _, err := p.gen.Generate(ctx, systemPrompt, prompt, "")
	if err != nil {
		return 0, fmt.Errorf("AI chain linking: %w", err)
	}
	aiDur := time.Since(t0).Round(time.Millisecond)
	if usage != nil {
		p.logger.Printf("chains: AI call done [%s] (%d+%d tokens, $%.4f, %d bytes response)",
			aiDur, usage.InputTokens, usage.OutputTokens, usage.CostUSD, len(resp))
	} else {
		p.logger.Printf("chains: AI call done [%s] (%d bytes response)", aiDur, len(resp))
	}

	// 5. Parse AI response.
	if p.OnProgress != nil {
		p.OnProgress(0, total, "Applying chain assignments...")
	}
	t0 = time.Now()
	assignments, err := parseResponse(resp)
	if err != nil {
		return 0, fmt.Errorf("parsing chain response: %w", err)
	}

	// Filter out low-confidence assignments to prevent false groupings.
	filtered := 0
	for i := range assignments {
		if assignments[i].Action != "SKIP" && assignments[i].Confidence < MinChainConfidence {
			p.logger.Printf("chains: rejecting %s assignment [%s%d] (confidence %.0f%% < %d%%): %s",
				assignments[i].Action, assignments[i].ItemType, assignments[i].DecisionIndex,
				assignments[i].Confidence, MinChainConfidence,
				assignments[i].Title)
			assignments[i].Action = "SKIP"
			filtered++
		}
	}
	if filtered > 0 {
		p.logger.Printf("chains: filtered %d low-confidence assignment(s)", filtered)
	}

	p.logger.Printf("chains: parsed %d assignments (%d accepted) [%s]", len(assignments), len(assignments)-filtered, time.Since(t0).Round(time.Millisecond))

	// 6. Apply assignments: create new chains, link decisions and digests.
	t0 = time.Now()
	linked := 0
	// Track chain slugs → IDs to prevent duplicates within this run.
	// Pre-populate with existing active chains so "NEW" with a duplicate slug reuses the existing chain.
	newChainIDs := make(map[string]int) // slug → chain ID
	for _, c := range activeChains {
		newChainIDs[c.Slug] = c.ID
	}

	for i, a := range assignments {
		if a.Action == "SKIP" {
			continue
		}

		if ctx.Err() != nil {
			return linked, ctx.Err()
		}

		if p.OnProgress != nil {
			p.OnProgress(i+1, len(assignments), fmt.Sprintf("Applying %d/%d...", i+1, len(assignments)))
		}

		if a.ItemType == "digest" {
			// Link digest to chain.
			linked += p.applyDigestAssignment(a, digContexts, activeChains, newChainIDs)
			continue
		}

		// Default: decision assignment.
		if a.DecisionIndex < 0 || a.DecisionIndex >= len(unlinked) {
			p.logger.Printf("chains: skipping assignment with out-of-bounds index %d (max %d)", a.DecisionIndex, len(unlinked)-1)
			continue
		}
		dec := unlinked[a.DecisionIndex]

		var chainID int
		if a.Action == "NEW" {
			// Deduplicate: if a chain with this slug was already created in this run, reuse it.
			if existingID, ok := newChainIDs[a.Slug]; ok {
				chainID = existingID
				p.logger.Printf("chains: reusing chain #%d %q for decision (same slug %q)", chainID, a.Title, a.Slug)
				if err := p.db.AddChannelToChain(chainID, dec.ChannelID); err != nil {
					p.logger.Printf("chains: add channel error: %v", err)
				}
			} else {
				id, err := p.db.CreateChain(db.Chain{
					ParentID:   a.ParentID,
					Title:      a.Title,
					Slug:       a.Slug,
					Status:     "active",
					Summary:    a.Summary,
					ChannelIDs: marshalStringSlice([]string{dec.ChannelID}),
					FirstSeen:  dec.PeriodTo,
					LastSeen:   dec.PeriodTo,
					ItemCount:  1,
				})
				if err != nil {
					p.logger.Printf("chains: create chain error: %v", err)
					continue
				}
				chainID = int(id)
				newChainIDs[a.Slug] = chainID
				p.logger.Printf("chains: created chain #%d %q", chainID, a.Title)
			}
		} else {
			// Link to existing chain — validate AI-returned ID exists in active or newly created chains.
			chainID = a.ChainID
			found := false
			for _, c := range activeChains {
				if c.ID == chainID {
					found = true
					break
				}
			}
			if !found {
				// Also check chains created during this run.
				for _, id := range newChainIDs {
					if id == chainID {
						found = true
						break
					}
				}
			}
			if !found {
				p.logger.Printf("chains: AI returned unknown chain_id %d, skipping", chainID)
				continue
			}
			if err := p.db.AddChannelToChain(chainID, dec.ChannelID); err != nil {
				p.logger.Printf("chains: add channel error: %v", err)
			}
		}

		// Insert chain ref.
		err := p.db.InsertChainRef(db.ChainRef{
			ChainID:     chainID,
			RefType:     "decision",
			DigestID:    dec.DigestID,
			DecisionIdx: dec.DecisionIdx,
			ChannelID:   dec.ChannelID,
			Timestamp:   dec.PeriodTo,
		})
		if err != nil {
			p.logger.Printf("chains: insert ref error: %v", err)
			continue
		}
		linked++

		// Update chain metadata.
		p.updateChainMetadata(chainID, dec.PeriodTo)
	}

	p.logger.Printf("chains: applied %d assignments [%s]", len(assignments), time.Since(t0).Round(time.Millisecond))

	// 7. Update summaries for chains that got new items (batch AI call).
	if linked > 0 {
		t0 = time.Now()
		p.updateChainSummaries(ctx, activeChains, assignments)
		p.logger.Printf("chains: updated summaries [%s]", time.Since(t0).Round(time.Millisecond))
	}

	// 8. Mark stale chains.
	if n, err := p.db.MarkStaleChains(cutoff); err != nil {
		p.logger.Printf("chains: mark stale error: %v", err)
	} else if n > 0 {
		p.logger.Printf("chains: marked %d chain(s) as stale", n)
	}

	p.logger.Printf("chains: total run time: %s", time.Since(runStart).Round(time.Millisecond))

	if p.OnProgress != nil {
		p.OnProgress(total, total, fmt.Sprintf("Linked %d item(s) to chains", linked))
	}

	return linked, nil
}

// applyDigestAssignment links a digest to a chain. Returns 1 if linked, 0 otherwise.
func (p *Pipeline) applyDigestAssignment(a assignment, digContexts []digestContext, activeChains []db.Chain, newChainIDs map[string]int) int {
	if a.DecisionIndex < 0 || a.DecisionIndex >= len(digContexts) {
		p.logger.Printf("chains: skipping digest assignment with out-of-bounds index %d", a.DecisionIndex)
		return 0
	}
	dig := digContexts[a.DecisionIndex]

	var chainID int
	if a.Action == "NEW" {
		// Deduplicate: if a chain with this slug was already created in this run, reuse it.
		if existingID, ok := newChainIDs[a.Slug]; ok {
			chainID = existingID
			p.logger.Printf("chains: reusing chain #%d %q for digest (same slug %q)", chainID, a.Title, a.Slug)
			if err := p.db.AddChannelToChain(chainID, dig.ChannelID); err != nil {
				p.logger.Printf("chains: add channel error: %v", err)
			}
		} else {
			id, err := p.db.CreateChain(db.Chain{
				ParentID:   a.ParentID,
				Title:      a.Title,
				Slug:       a.Slug,
				Status:     "active",
				Summary:    a.Summary,
				ChannelIDs: marshalStringSlice([]string{dig.ChannelID}),
				FirstSeen:  dig.PeriodTo,
				LastSeen:   dig.PeriodTo,
				ItemCount:  1,
			})
			if err != nil {
				p.logger.Printf("chains: create chain for digest error: %v", err)
				return 0
			}
			chainID = int(id)
			newChainIDs[a.Slug] = chainID
			p.logger.Printf("chains: created chain #%d %q (from digest)", chainID, a.Title)
		}
	} else {
		// Link to existing chain — validate AI-returned ID exists in active or newly created chains.
		chainID = a.ChainID
		found := false
		for _, c := range activeChains {
			if c.ID == chainID {
				found = true
				break
			}
		}
		if !found {
			for _, id := range newChainIDs {
				if id == chainID {
					found = true
					break
				}
			}
		}
		if !found {
			p.logger.Printf("chains: AI returned unknown chain_id %d for digest, skipping", chainID)
			return 0
		}
		if err := p.db.AddChannelToChain(chainID, dig.ChannelID); err != nil {
			p.logger.Printf("chains: add channel error: %v", err)
		}
	}

	err := p.db.InsertChainRef(db.ChainRef{
		ChainID:   chainID,
		RefType:   "digest",
		DigestID:  dig.DigestID,
		ChannelID: dig.ChannelID,
		Timestamp: dig.PeriodTo,
	})
	if err != nil {
		p.logger.Printf("chains: insert digest ref error: %v", err)
		return 0
	}

	p.updateChainMetadata(chainID, dig.PeriodTo)
	return 1
}

// updateChainMetadata refreshes item_count and last_seen for a chain.
func (p *Pipeline) updateChainMetadata(chainID int, timestamp float64) {
	count, _ := p.db.GetChainItemCount(chainID)
	chain, err := p.db.GetChainByID(chainID)
	if err == nil {
		lastSeen := chain.LastSeen
		if timestamp > lastSeen {
			lastSeen = timestamp
		}
		_ = p.db.UpdateChainSummary(chainID, chain.Summary, lastSeen, count, chain.ChannelIDs)
	}
}

// FormatActiveChainsForPrompt formats active chains as a text section for other pipelines.
// Used by tracks pipeline and rollup pipeline to inject chain context.
func (p *Pipeline) FormatActiveChainsForPrompt(ctx context.Context) (string, error) {
	chains, err := p.db.GetActiveChains(DefaultStaleDays)
	if err != nil {
		return "", err
	}
	if len(chains) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("=== ACTIVE CHAINS ===\n")
	sb.WriteString("These are ongoing discussion threads across channels.\n\n")
	for _, c := range chains {
		prefix := ""
		if c.ParentID > 0 {
			prefix = "  └─ "
		}
		sb.WriteString(fmt.Sprintf("%sChain #%d: %q — %s\n", prefix, c.ID, c.Title, c.Summary))
	}
	return sb.String(), nil
}

// FormatChainedDecisionsForRollup groups chained decisions for a daily/weekly rollup.
// Returns two strings: chained decisions section and standalone decisions section.
func (p *Pipeline) FormatChainedDecisionsForRollup(decisions []db.UnlinkedDecision, allDecisions []rollupDecision) (chained string, standalone string, err error) {
	// Get all chain refs for recent decisions.
	chains, err := p.db.GetActiveChains(DefaultStaleDays)
	if err != nil {
		return "", "", err
	}
	if len(chains) == 0 {
		return "", "", nil // no chains, all decisions are standalone
	}

	// Build chain ID → chain map.
	chainMap := make(map[int]*db.Chain, len(chains))
	for i := range chains {
		chainMap[chains[i].ID] = &chains[i]
	}

	// Group rollup decisions by chain.
	type chainGroup struct {
		chain *db.Chain
		items []rollupDecision
	}
	groups := make(map[int]*chainGroup)
	var standaloneItems []rollupDecision

	for _, d := range allDecisions {
		chainID := p.db.IsDecisionChained(d.DigestID, d.DecisionIdx)
		if chainID > 0 {
			if g, ok := groups[chainID]; ok {
				g.items = append(g.items, d)
			} else if c, ok := chainMap[chainID]; ok {
				groups[chainID] = &chainGroup{chain: c, items: []rollupDecision{d}}
			}
		} else {
			standaloneItems = append(standaloneItems, d)
		}
	}

	// Format chained section.
	if len(groups) > 0 {
		var sb strings.Builder
		sb.WriteString("=== CHAIN UPDATES ===\n")
		sb.WriteString("The following decisions are part of ongoing chains. Do NOT repeat them individually.\n")
		sb.WriteString("Instead, summarize what changed in each chain.\n\n")
		for _, g := range groups {
			sb.WriteString(fmt.Sprintf("Chain %q (%d total items, %d new):\n", g.chain.Title, g.chain.ItemCount, len(g.items)))
			for _, d := range g.items {
				sb.WriteString(fmt.Sprintf("  - [#%s] %q (%s)\n", d.ChannelName, d.Text, d.Importance))
			}
			sb.WriteString("\n")
		}
		chained = sb.String()
	}

	// Format standalone section.
	if len(standaloneItems) > 0 {
		var sb strings.Builder
		sb.WriteString("=== STANDALONE DECISIONS ===\n")
		sb.WriteString("These are NOT part of any chain. Include them normally.\n\n")
		for _, d := range standaloneItems {
			sb.WriteString(fmt.Sprintf("  - [#%s] %q (%s)\n", d.ChannelName, d.Text, d.Importance))
		}
		standalone = sb.String()
	}

	return chained, standalone, nil
}

// rollupDecision represents a decision being included in a rollup.
type rollupDecision struct {
	DigestID    int
	DecisionIdx int
	ChannelName string
	Text        string
	Importance  string
}

// updateChainSummaries regenerates summaries for chains that received new items.
func (p *Pipeline) updateChainSummaries(ctx context.Context, existingChains []db.Chain, assignments []assignment) {
	// Collect chain IDs that got updates (only EXISTING — NEW chains already have summary from creation).
	updatedIDs := make(map[int]bool)
	for _, a := range assignments {
		if a.Action == "EXISTING" {
			updatedIDs[a.ChainID] = true
		}
	}
	for _, c := range existingChains {
		if ctx.Err() != nil {
			return
		}
		if !updatedIDs[c.ID] {
			continue
		}
		count, _ := p.db.GetChainItemCount(c.ID)
		_ = p.db.UpdateChainSummary(c.ID, c.Summary, c.LastSeen, count, c.ChannelIDs)
	}
}

const chainsSystemPrompt = `You are organizing workspace decisions and digest summaries into thematic chains.
A chain groups related decisions and digests that are about the SAME SPECIFIC topic, project, or initiative — even across different channels.

Chains can be hierarchical: a PARENT chain is an umbrella for a large initiative (e.g. "EU Launch"),
and CHILD chains are sub-topics within it (e.g. "EU Fireblocks Integration", "EU AML Compliance").

CRITICAL — CHAIN SPECIFICITY:
A chain MUST represent a SPECIFIC initiative, project, decision thread, or concrete topic.
NEVER create broad umbrella chains around general domains like "Security", "Infrastructure", "Operations", "Performance", "Access Control", "Governance", "Code Quality", "DevOps", etc.

BAD chain (too broad): "Access Control & Security Governance" grouping:
  - replacing a vulnerability scanner tool
  - removing companies from a third-party registry
  - AI data policy changes
  - investigating a release bug
These are 4 UNRELATED topics that happen to touch "security" loosely. They should be separate chains or SKIPped.

GOOD chains (specific):
  - "Nessus Migration" — replacing OpenVas with Nessus across scanning infrastructure
  - "AI Data Governance Policy" — establishing rules for sensitive data in AI systems
  - "Q1 Third-Party Vendor Audit" — reviewing and cleaning the third-party registry

IMPORTANT RULES:
1. Same keyword in different contexts = DIFFERENT chains. "database migration to RDS" ≠ "database backup policy".
2. Match by semantic meaning AND concrete project context, not just domain keywords.
3. A decision/digest can belong to at most one chain.
4. PREFER SKIP over creating overly broad chains. One-off decisions or loosely related items should be SKIPped.
5. When creating a NEW chain, the title should name the SPECIFIC initiative or decision thread (5-10 words).
6. The slug should be a lowercase-kebab-case identifier derived from the title.
7. PREFER adding to EXISTING chains over creating new ones. Only create a new chain if the topic is genuinely distinct.
8. If multiple related chains could share a parent theme, create a PARENT chain and set parent_id on the children.
9. Digests provide rich context (summaries, topics) — use them to better understand the theme of a channel's activity.
10. Ask yourself: "Would these items appear in the same status update or project tracker?" If not, they don't belong in one chain.

CONFIDENCE SCORING:
Every assignment MUST include a "confidence" field (0-100) reflecting how certain you are that this item belongs to this chain.
- 90-100: Clear match — same project, same initiative, directly related participants/topics
- 70-89: Likely match — strong thematic overlap, shared context
- 50-69: Weak match — surface-level keyword overlap but different contexts (e.g. "developer" in DevOps vs HR)
- 0-49: No real match — different domains, coincidental keyword overlap
Items with confidence below 70 will be automatically rejected. When in doubt, SKIP.

Return a JSON array with one entry per item (decisions first, then digests).`

func (p *Pipeline) buildPrompt(chains []db.Chain, unlinked []db.UnlinkedDecision, digContexts []digestContext) string {
	var sb strings.Builder

	// Active chains section (with parent/child info).
	if len(chains) > 0 {
		sb.WriteString("=== ACTIVE CHAINS ===\n")
		for _, c := range chains {
			var channelIDs []string
			_ = json.Unmarshal([]byte(c.ChannelIDs), &channelIDs)
			parentInfo := ""
			if c.ParentID > 0 {
				parentInfo = fmt.Sprintf(" (child of #%d)", c.ParentID)
			}
			sb.WriteString(fmt.Sprintf("Chain #%d: %q (slug: %s)%s\n  Summary: %s\n  Channels: %s\n  Items: %d\n\n",
				c.ID, c.Title, c.Slug, parentInfo, c.Summary, strings.Join(channelIDs, ", "), c.ItemCount))
		}
	} else {
		sb.WriteString("=== ACTIVE CHAINS ===\nNo active chains yet. Create new ones as needed.\n\n")
	}

	// Unlinked decisions section.
	sb.WriteString("=== UNLINKED DECISIONS ===\n")
	for i, d := range unlinked {
		channelLabel := d.ChannelName
		if channelLabel == "" {
			channelLabel = d.ChannelID
		}
		sb.WriteString(fmt.Sprintf("[D%d] Channel: #%s | Importance: %s | By: %s\n    %q\n\n",
			i, channelLabel, d.Importance, d.DecisionBy, d.DecisionText))
	}

	// Unlinked digests section — provides richer context for grouping.
	if len(digContexts) > 0 {
		sb.WriteString("=== UNLINKED DIGESTS ===\n")
		sb.WriteString("These are channel digest summaries. Link them to chains to provide richer context.\n\n")
		for i, d := range digContexts {
			channelLabel := d.ChannelName
			if channelLabel == "" {
				channelLabel = d.ChannelID
			}
			sb.WriteString(fmt.Sprintf("[G%d] Channel: #%s | Topics: %s\n    Summary: %s\n\n",
				i, channelLabel, d.Topics, d.Summary))
		}
	}

	// Language instruction.
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		sb.WriteString(fmt.Sprintf("\nIMPORTANT: Write ALL text values (title, summary) in %s.\n\n", lang))
	}

	sb.WriteString(`Return a JSON array. For each decision (D-prefixed index) and digest (G-prefixed index), specify one of:
- {"index": 0, "item_type": "decision", "action": "EXISTING", "chain_id": 5, "confidence": 85}
- {"index": 1, "item_type": "decision", "action": "NEW", "title": "Short title", "slug": "kebab-slug", "summary": "One sentence", "parent_id": 0, "confidence": 92}
- {"index": 0, "item_type": "digest", "action": "EXISTING", "chain_id": 5, "confidence": 78}
- {"index": 1, "item_type": "digest", "action": "NEW", "title": "Short title", "slug": "kebab-slug", "summary": "One sentence", "parent_id": 0, "confidence": 90}
- {"index": 2, "item_type": "decision", "action": "SKIP", "confidence": 0}

Set parent_id to an existing chain ID to make a child chain, or 0 for top-level.
Every non-SKIP assignment MUST have confidence >= 70 to be accepted.
`)
	return sb.String()
}

// MinChainConfidence is the minimum confidence score (0-100) for an AI chain assignment to be accepted.
// Assignments below this threshold are treated as SKIP to prevent false groupings.
const MinChainConfidence = 70

// assignment is a parsed AI response for a single decision or digest.
type assignment struct {
	DecisionIndex int     `json:"index"`
	ItemType      string  `json:"item_type,omitempty"` // "decision" or "digest" (default "decision")
	Action        string  `json:"action"`              // "EXISTING", "NEW", "SKIP"
	ChainID       int     `json:"chain_id,omitempty"`
	ParentID      int     `json:"parent_id,omitempty"`
	Title         string  `json:"title,omitempty"`
	Slug          string  `json:"slug,omitempty"`
	Summary       string  `json:"summary,omitempty"`
	Confidence    float64 `json:"confidence"` // 0-100 confidence score
}

func parseResponse(resp string) ([]assignment, error) {
	// Extract JSON from response (may have markdown fences).
	resp = strings.TrimSpace(resp)
	if idx := strings.Index(resp, "["); idx >= 0 {
		resp = resp[idx:]
	}
	if idx := strings.LastIndex(resp, "]"); idx >= 0 {
		resp = resp[:idx+1]
	}

	var assignments []assignment
	if err := json.Unmarshal([]byte(resp), &assignments); err != nil {
		return nil, fmt.Errorf("parsing chain assignments JSON: %w", err)
	}
	return assignments, nil
}

func marshalStringSlice(ss []string) string {
	data, _ := json.Marshal(ss)
	return string(data)
}
