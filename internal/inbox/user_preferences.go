package inbox

import (
	"fmt"
	"sort"
	"strings"

	"watchtower/internal/db"
)

const maxPrefsInPrompt = 20

// buildUserPreferencesBlock returns a formatted "=== USER PREFERENCES ===" section
// containing learned rules relevant to the given items' senders/channels, capped at maxPrefsInPrompt.
// Returns an empty string when no matching rules exist.
func buildUserPreferencesBlock(database *db.DB, items []db.InboxItem) (string, error) {
	seen := map[string]bool{}
	var scopes []string
	for _, it := range items {
		add := func(s string) {
			if !seen[s] {
				seen[s] = true
				scopes = append(scopes, s)
			}
		}
		add("sender:" + it.SenderUserID)
		add("channel:" + it.ChannelID)
	}
	if len(scopes) == 0 {
		return "", nil
	}

	rules, err := database.ListLearnedRulesByScope(scopes, maxPrefsInPrompt)
	if err != nil {
		return "", err
	}
	if len(rules) == 0 {
		return "", nil
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return absF(rules[i].Weight) > absF(rules[j].Weight)
	})

	var mutes, boosts []string
	for _, r := range rules {
		line := fmt.Sprintf("%s (weight=%.1f, %s)", r.ScopeKey, r.Weight, r.Source)
		if r.Weight < 0 {
			mutes = append(mutes, line)
		} else {
			boosts = append(boosts, line)
		}
	}

	var b strings.Builder
	b.WriteString("=== USER PREFERENCES ===\n")
	if len(mutes) > 0 {
		b.WriteString("Mutes: " + strings.Join(mutes, "; ") + "\n")
	}
	if len(boosts) > 0 {
		b.WriteString("Boosts: " + strings.Join(boosts, "; ") + "\n")
	}
	b.WriteString("Apply these when choosing priority and selecting pinned items.\n")
	return b.String(), nil
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
