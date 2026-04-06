package jira

import (
	"context"
	"log"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"watchtower/internal/db"
)

// UserMapper resolves Jira users to Slack users.
type UserMapper struct {
	client *Client
	db     *db.DB
	logger *log.Logger
}

// NewUserMapper creates a UserMapper.
func NewUserMapper(client *Client, database *db.DB) *UserMapper {
	return &UserMapper{
		client: client,
		db:     database,
		logger: log.New(os.Stderr, "[jira-users] ", log.LstdFlags),
	}
}

// SetLogger replaces the mapper's logger.
func (m *UserMapper) SetLogger(l *log.Logger) {
	m.logger = l
}

// ResolveAll resolves Jira users to Slack users using:
// Phase 1: email matching
// Phase 2: fuzzy name matching (>0.85 threshold)
// Phase 3: manual mapping from config
func (m *UserMapper) ResolveAll(ctx context.Context, manualMap map[string]string) error {
	// Get all unmapped Jira users from issues.
	maps, err := m.db.GetJiraUserMaps()
	if err != nil {
		return err
	}

	// Get all Slack users for matching.
	slackUsers, err := m.db.GetUsers(db.UserFilter{})
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	for i := range maps {
		mapping := &maps[i]
		if mapping.SlackUserID != "" {
			continue // already resolved
		}

		// Phase 1: Email match.
		if mapping.Email != "" {
			for _, su := range slackUsers {
				if strings.EqualFold(su.Email, mapping.Email) {
					mapping.SlackUserID = su.ID
					mapping.MatchMethod = "email"
					mapping.MatchConfidence = 1.0
					mapping.ResolvedAt = now
					break
				}
			}
		}

		// Phase 2: Fuzzy name match (if email didn't work).
		if mapping.SlackUserID == "" && mapping.DisplayName != "" {
			bestScore := 0.0
			bestID := ""
			for _, su := range slackUsers {
				if su.IsBot || su.IsDeleted {
					continue
				}
				names := []string{su.RealName, su.DisplayName, su.Name}
				for _, name := range names {
					if name == "" {
						continue
					}
					score := fuzzyMatch(mapping.DisplayName, name)
					if score > bestScore {
						bestScore = score
						bestID = su.ID
					}
				}
			}
			if bestScore > 0.85 && bestID != "" {
				mapping.SlackUserID = bestID
				mapping.MatchMethod = "display_name"
				mapping.MatchConfidence = bestScore
				mapping.ResolvedAt = now
			}
		}

		// Phase 3: Manual mapping override.
		if slackID, ok := manualMap[mapping.JiraAccountID]; ok {
			mapping.SlackUserID = slackID
			mapping.MatchMethod = "manual"
			mapping.MatchConfidence = 1.0
			mapping.ResolvedAt = now
		}

		if err := m.db.UpsertJiraUserMap(*mapping); err != nil {
			m.logger.Printf("failed to upsert user map for %s: %v", mapping.JiraAccountID, err)
		}
	}

	return nil
}

// ResolveOne returns the cached mapping for a Jira account ID.
func (m *UserMapper) ResolveOne(ctx context.Context, accountID string) (*db.JiraUserMap, error) {
	return m.db.GetJiraUserMapByAccountID(accountID)
}

// fuzzyMatch returns a normalized Levenshtein similarity between two strings (0.0 to 1.0).
func fuzzyMatch(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return 1.0
	}

	lenA := utf8.RuneCountInString(a)
	lenB := utf8.RuneCountInString(b)
	maxLen := lenA
	if lenB > maxLen {
		maxLen = lenB
	}
	if maxLen == 0 {
		return 1.0
	}

	dist := levenshteinDistance([]rune(a), []rune(b))
	return 1.0 - float64(dist)/float64(maxLen)
}

// levenshteinDistance computes the edit distance between two rune slices.
func levenshteinDistance(a, b []rune) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = int(math.Min(
				math.Min(float64(curr[j-1]+1), float64(prev[j]+1)),
				float64(prev[j-1]+cost),
			))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
