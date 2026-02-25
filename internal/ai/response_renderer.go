package ai

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"

	"watchtower/internal/db"
	slackutil "watchtower/internal/slack"
)

// messageRef represents a detected message reference in the AI response.
type messageRef struct {
	// fullMatch is the matched text in the response (e.g., "#general 2025-02-24 14:30")
	fullMatch string
	// channelName is the extracted channel name
	channelName string
	// timestamp is the parsed time from the reference
	timestamp time.Time
	// permalink is the resolved Slack permalink (empty if unresolved)
	permalink string
}

// ResponseRenderer processes Claude's raw response text, resolves message
// references to Slack permalinks, renders markdown for terminal display, and
// appends a Sources section with referenced message links.
type ResponseRenderer struct {
	db     *db.DB
	domain string
}

// NewResponseRenderer creates a ResponseRenderer.
func NewResponseRenderer(database *db.DB, domain string) *ResponseRenderer {
	return &ResponseRenderer{
		db:     database,
		domain: domain,
	}
}

// refPattern matches message references like "#channel-name 2025-02-24 14:30"
// in the AI response. The channel name can contain alphanumerics, hyphens, and underscores.
var refPattern = regexp.MustCompile(`#([a-zA-Z0-9][a-zA-Z0-9_-]*)\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2})`)

// Render processes the AI response:
//  1. Detects message references (patterns like #channel-name YYYY-MM-DD HH:MM)
//  2. Looks up matching messages in the DB and converts references to Slack permalinks
//  3. Renders markdown via glamour with a dark terminal theme
//  4. Appends a Sources section listing all referenced messages with links
func (r *ResponseRenderer) Render(response string) (string, error) {
	refs := r.extractRefs(response)
	resolved := r.resolveRefs(refs)

	// Replace references with linked text in the markdown
	processed := r.replaceRefs(response, resolved)

	// Render markdown for terminal
	rendered, err := r.renderMarkdown(processed)
	if err != nil {
		// Fall back to unrendered text if glamour fails
		rendered = processed
	}

	// Append sources section if there are resolved references
	sources := r.buildSourcesSection(resolved)
	if sources != "" {
		rendered = strings.TrimRight(rendered, "\n") + "\n\n" + sources
	}

	return rendered, nil
}

// extractRefs finds all message reference patterns in the response text.
func (r *ResponseRenderer) extractRefs(response string) []messageRef {
	matches := refPattern.FindAllStringSubmatch(response, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var refs []messageRef
	for _, m := range matches {
		fullMatch := m[0]
		if seen[fullMatch] {
			continue
		}
		seen[fullMatch] = true

		channelName := m[1]
		timeStr := m[2]

		t, err := time.Parse("2006-01-02 15:04", timeStr)
		if err != nil {
			continue
		}

		refs = append(refs, messageRef{
			fullMatch:   fullMatch,
			channelName: channelName,
			timestamp:   t,
		})
	}
	return refs
}

// resolveRefs looks up each reference in the database and fills in the permalink.
func (r *ResponseRenderer) resolveRefs(refs []messageRef) []messageRef {
	var resolved []messageRef
	for _, ref := range refs {
		ch, err := r.db.GetChannelByName(ref.channelName)
		if err != nil || ch == nil {
			continue
		}

		tsUnix := float64(ref.timestamp.Unix())
		msg, err := r.db.GetMessageNear(ch.ID, tsUnix)
		if err != nil || msg == nil {
			continue
		}

		ref.permalink = slackutil.GeneratePermalink(r.domain, ch.ID, msg.TS)
		resolved = append(resolved, ref)
	}
	return resolved
}

// replaceRefs replaces raw message references with markdown links in the response text.
func (r *ResponseRenderer) replaceRefs(response string, refs []messageRef) string {
	for _, ref := range refs {
		if ref.permalink == "" {
			continue
		}
		linked := fmt.Sprintf("[%s](%s)", ref.fullMatch, ref.permalink)
		response = strings.Replace(response, ref.fullMatch, linked, 1)
	}
	return response
}

// renderMarkdown renders the response using glamour with a dark terminal theme.
func (r *ResponseRenderer) renderMarkdown(text string) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return "", fmt.Errorf("creating markdown renderer: %w", err)
	}
	out, err := renderer.Render(text)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	return out, nil
}

// buildSourcesSection creates the Sources footer listing resolved message references.
func (r *ResponseRenderer) buildSourcesSection(refs []messageRef) string {
	var linked []messageRef
	for _, ref := range refs {
		if ref.permalink != "" {
			linked = append(linked, ref)
		}
	}
	if len(linked) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Sources:\n")
	for i, ref := range linked {
		b.WriteString(fmt.Sprintf("  [%d] #%s %s — %s\n",
			i+1,
			ref.channelName,
			ref.timestamp.Format("2006-01-02 15:04"),
			ref.permalink,
		))
	}
	return b.String()
}
