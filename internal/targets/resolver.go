// Package targets provides AI-powered extraction and linking of hierarchical goals.
package targets

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"watchtower/internal/db"
	"watchtower/internal/jira"
)

// URLResolver resolves a single URL match to an Enrichment.
type URLResolver interface {
	CanResolve(url string) bool
	Resolve(ctx context.Context, match URLMatch) (*Enrichment, error)
}

// URLMatch holds a detected URL and parsed metadata.
type URLMatch struct {
	Source    string // "slack" or "jira"
	RawURL    string
	ChannelID string // Slack only
	TS        string // Slack only (normalized: "1714567890.123456")
	Key       string // Jira only (e.g. "PROJ-123")
}

// Enrichment is the result of resolving a URL.
type Enrichment struct {
	Ref    string // "jira:PROJ-123" or "slack:C123:1714567890.123456"
	Title  string
	Body   string // formatted text for prompt injection
	Source string // "local" or "mcp"
	Error  string // non-empty if resolution failed; pipeline proceeds
}

// MCPClient is an injectable interface for fetching Jira issues via MCP.
// Kept minimal for V1; implementations provide real MCP calls.
type MCPClient interface {
	GetJiraIssue(ctx context.Context, key string) (*db.JiraIssue, error)
}

var (
	// slackPermalinkRe matches Slack archive permalinks.
	// Format: https://<workspace>.slack.com/archives/<C_OR_D_ID>/p<digits>[?thread_ts=<digits>.<digits>]
	slackPermalinkRe = regexp.MustCompile(
		`https://[a-z0-9-]+\.slack\.com/archives/([A-Z0-9]+)/p(\d{10})(\d{6})(?:\?[^\s"]*)?`,
	)

	// jiraIssueRe matches Jira browse URLs.
	// Format: https://<org>.atlassian.net/browse/<KEY-NUM>
	jiraIssueRe = regexp.MustCompile(
		`https://[a-z0-9-]+\.atlassian\.net/browse/([A-Z][A-Z0-9]+-\d+)`,
	)
)

// Extract detects Slack and Jira URLs in text and returns URLMatch entries.
// Only the two supported URL types (Slack archives, Jira browse) are returned.
func Extract(text string) []URLMatch {
	var matches []URLMatch
	seen := make(map[string]bool)

	for _, m := range slackPermalinkRe.FindAllStringSubmatch(text, -1) {
		raw := m[0]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		channelID := m[1]
		// Convert p<10digits><6digits> -> "xxxxxxxxxx.yyyyyy"
		ts := m[2] + "." + m[3]
		matches = append(matches, URLMatch{
			Source:    "slack",
			RawURL:    raw,
			ChannelID: channelID,
			TS:        ts,
		})
	}

	for _, m := range jiraIssueRe.FindAllStringSubmatch(text, -1) {
		raw := m[0]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		matches = append(matches, URLMatch{
			Source: "jira",
			RawURL: raw,
			Key:    m[1],
		})
	}

	return matches
}

// Resolver holds registered URLResolvers and dispatches per match.
type Resolver struct {
	db        *db.DB
	mcp       MCPClient // nil = no MCP fallback
	resolvers []URLResolver
	timeout   time.Duration
}

// NewResolver creates a Resolver with Slack and Jira resolvers registered.
// mcp may be nil to disable MCP fallback.
func NewResolver(database *db.DB, mcp MCPClient, mcpTimeout time.Duration) *Resolver {
	if mcpTimeout <= 0 {
		mcpTimeout = 10 * time.Second
	}
	r := &Resolver{
		db:      database,
		mcp:     mcp,
		timeout: mcpTimeout,
	}
	r.resolvers = []URLResolver{
		&SlackResolver{db: database},
		&JiraResolver{db: database, mcp: mcp, timeout: mcpTimeout},
	}
	return r
}

// Resolve resolves a slice of URLMatches concurrently (sequential per match, 10s timeout each).
// Errors are non-fatal: a failed enrichment has Error populated.
func (r *Resolver) Resolve(ctx context.Context, matches []URLMatch) []Enrichment {
	enrichments := make([]Enrichment, 0, len(matches))
	for _, m := range matches {
		for _, res := range r.resolvers {
			if !res.CanResolve(m.RawURL) {
				continue
			}
			mctx, cancel := context.WithTimeout(ctx, r.timeout)
			e, err := res.Resolve(mctx, m)
			cancel()
			if err != nil {
				enrichments = append(enrichments, Enrichment{
					Ref:   refForMatch(m),
					Error: err.Error(),
				})
			} else if e != nil {
				enrichments = append(enrichments, *e)
			}
			break // first matching resolver wins
		}
	}
	return enrichments
}

// refForMatch builds the canonical ref string for a URLMatch.
func refForMatch(m URLMatch) string {
	switch m.Source {
	case "slack":
		return fmt.Sprintf("slack:%s:%s", m.ChannelID, m.TS)
	case "jira":
		return fmt.Sprintf("jira:%s", m.Key)
	default:
		return m.RawURL
	}
}

// SlackResolver resolves Slack permalink URLs from the local messages table.
type SlackResolver struct {
	db *db.DB
}

// CanResolve returns true for Slack permalink URLs.
func (s *SlackResolver) CanResolve(url string) bool {
	return slackPermalinkRe.MatchString(url)
}

// Resolve looks up the message in the local DB and formats an Enrichment.
func (s *SlackResolver) Resolve(ctx context.Context, m URLMatch) (*Enrichment, error) {
	ref := fmt.Sprintf("slack:%s:%s", m.ChannelID, m.TS)

	type msgRow struct {
		Text      string
		UserID    string
		ChannelID string
		ThreadTS  string
	}

	var row msgRow
	err := s.db.QueryRowContext(ctx,
		`SELECT text, user_id, channel_id, COALESCE(thread_ts,'') FROM messages WHERE channel_id = ? AND ts = ?`,
		m.ChannelID, m.TS,
	).Scan(&row.Text, &row.UserID, &row.ChannelID, &row.ThreadTS)

	if err != nil {
		// Not found — annotate gracefully; no error returned to caller.
		return &Enrichment{
			Ref:    ref,
			Body:   "[slack url not in local DB]",
			Source: "local",
		}, nil
	}

	// Resolve display name.
	userName := row.UserID
	if name, nerr := s.db.UserNameByID(row.UserID); nerr == nil && name != "" {
		userName = name
	}

	// Resolve channel name.
	channelName := m.ChannelID
	if cn, cerr := s.db.ChannelNameByID(m.ChannelID); cerr == nil && cn != "" {
		channelName = cn
	}

	// Parse timestamp for display (ts is unix seconds dot microseconds).
	displayTime := ""
	if parts := strings.SplitN(m.TS, ".", 2); len(parts) == 2 {
		var sec int64
		if _, serr := fmt.Sscan(parts[0], &sec); serr == nil {
			displayTime = time.Unix(sec, 0).UTC().Format("2006-01-02 15:04")
		}
	}

	body := truncateRunes(row.Text, 500)

	formatted := fmt.Sprintf("#%s by @%s at %s\n%s", channelName, userName, displayTime, body)
	return &Enrichment{
		Ref:    ref,
		Title:  fmt.Sprintf("#%s: %s", channelName, truncate(body, 60)),
		Body:   formatted,
		Source: "local",
	}, nil
}

// JiraResolver resolves Jira issue URLs from the local jira_issues table,
// falling back to MCPClient if the issue is not cached locally.
type JiraResolver struct {
	db      *db.DB
	mcp     MCPClient
	timeout time.Duration
}

// CanResolve returns true for Jira browse URLs.
func (j *JiraResolver) CanResolve(url string) bool {
	return jiraIssueRe.MatchString(url)
}

// Resolve looks up the Jira issue, falling back to MCP on a local cache miss.
func (j *JiraResolver) Resolve(ctx context.Context, m URLMatch) (*Enrichment, error) {
	ref := fmt.Sprintf("jira:%s", m.Key)

	// Try local DB first.
	var issue db.JiraIssue
	err := j.db.QueryRowContext(ctx,
		`SELECT key, summary, status, priority,
		        COALESCE(assignee_display_name,''), COALESCE(sprint_name,''),
		        COALESCE(due_date,''), COALESCE(epic_key,''), COALESCE(status_category,'')
		 FROM jira_issues WHERE key = ? AND is_deleted = 0`,
		m.Key,
	).Scan(&issue.Key, &issue.Summary, &issue.Status, &issue.Priority,
		&issue.AssigneeDisplayName, &issue.SprintName,
		&issue.DueDate, &issue.EpicKey, &issue.StatusCategory)

	if err == nil {
		body := jira.BuildIssueContext([]db.JiraIssue{issue})
		return &Enrichment{
			Ref:    ref,
			Title:  fmt.Sprintf("[%s] %s", issue.Key, issue.Summary),
			Body:   body,
			Source: "local",
		}, nil
	}

	// Local miss — try MCP if available.
	if j.mcp == nil {
		return &Enrichment{
			Ref:    ref,
			Body:   "[jira fetch failed: not in local DB and MCP not configured]",
			Source: "local",
			Error:  "",
		}, nil
	}

	mcpCtx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	fetched, mcpErr := j.mcp.GetJiraIssue(mcpCtx, m.Key)
	if mcpErr != nil {
		reason := mcpErr.Error()
		if mcpCtx.Err() != nil {
			reason = "timeout"
		}
		return &Enrichment{
			Ref:    ref,
			Body:   fmt.Sprintf("[jira fetch failed: %s]", reason),
			Source: "mcp",
			Error:  "",
		}, nil
	}

	body := jira.BuildIssueContext([]db.JiraIssue{*fetched})
	return &Enrichment{
		Ref:    ref,
		Title:  fmt.Sprintf("[%s] %s", fetched.Key, fetched.Summary),
		Body:   body + " (via MCP)",
		Source: "mcp",
	}, nil
}

// truncateRunes returns s truncated to at most n runes, safe for multi-byte UTF-8.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// truncate returns s truncated to n runes with "..." appended if cut.
func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return truncateRunes(s, n) + "..."
}
