// Package tracks provides the tracks v3 pipeline — auto-creation and update of informational tracks.
package tracks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// ProgressFunc is called during pipeline execution to report progress.
type ProgressFunc func(done, total int, status string)

// Pipeline auto-creates and updates informational tracks from digest topics.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	OnProgress ProgressFunc

	// LastStep* fields are set before each OnProgress callback.
	LastStepMessageCount    int
	LastStepPeriodFrom      time.Time
	LastStepPeriodTo        time.Time
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int
	LastStepCostUSD         float64

	// Accumulated token usage across all Generate calls (thread-safe).
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops
	totalAPITokens    atomic.Int64

	// caches (populated once per Run)
	cacheMu      sync.RWMutex
	channelNames map[string]string
	userNames    map[string]string
	profile      *db.UserProfile
}

// New creates a new tracks pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{
		db:        database,
		cfg:       cfg,
		generator: gen,
		logger:    logger,
	}
}

// SetPromptStore sets an optional prompt store for loading customized prompts.
func (p *Pipeline) SetPromptStore(store *prompts.Store) {
	p.promptStore = store
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()),
		float64(p.totalCostMicro.Load()) / 1e6, int(p.totalAPITokens.Load())
}

// Run loads unlinked topics, existing tracks, and calls AI to create/update tracks.
// Returns (created, updated, error).
func (p *Pipeline) Run(ctx context.Context) (int, int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, 0, nil
	}

	p.loadCaches()

	// 1. Load unlinked topics (14 days).
	sinceUnix := float64(time.Now().AddDate(0, 0, -14).Unix())
	topics, err := p.db.GetUnlinkedTopics(sinceUnix)
	if err != nil {
		return 0, 0, fmt.Errorf("loading unlinked topics: %w", err)
	}

	// 2. Filter muted channels.
	mutedIDs, err := p.db.GetMutedChannelIDs()
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load muted channels: %v", err)
	} else if len(mutedIDs) > 0 {
		muted := make(map[string]bool, len(mutedIDs))
		for _, id := range mutedIDs {
			muted[id] = true
		}
		var filtered []db.UnlinkedTopic
		for _, t := range topics {
			if !muted[t.ChannelID] {
				filtered = append(filtered, t)
			}
		}
		if skipped := len(topics) - len(filtered); skipped > 0 {
			p.logger.Printf("tracks: skipped %d topic(s) from muted channels", skipped)
		}
		topics = filtered
	}

	// 3. Load existing tracks.
	existingTracks, err := p.db.GetAllActiveTracks()
	if err != nil {
		return 0, 0, fmt.Errorf("loading existing tracks: %w", err)
	}

	// 4. Load channel running summaries for context.
	channelSummaries := p.loadChannelSummaries(topics)

	// 5. If no topics — skip.
	if len(topics) == 0 {
		p.logger.Println("tracks: no unlinked topics found")
		return 0, 0, nil
	}

	p.logger.Printf("tracks: %d unlinked topics, %d existing tracks", len(topics), len(existingTracks))
	p.progress(0, 1, fmt.Sprintf("Processing %d topics...", len(topics)))

	// 6. Build prompt.
	existingTracksStr := p.formatExistingTracks(existingTracks)
	topicsStr := p.formatUnlinkedTopics(topics)

	promptTmpl, promptVersion := p.getPrompt(prompts.TracksCreate)
	langInstr := p.languageInstruction()
	langReminder := "IMPORTANT: Write ALL text values in the same language as the source messages."

	systemPrompt := fmt.Sprintf(promptTmpl,
		langInstr,
		existingTracksStr,
		topicsStr,
		channelSummaries,
		langReminder,
	)

	// 7. Single AI call.
	start := time.Now()
	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "tracks.create"), systemPrompt, "Generate tracks from the unlinked topics.", "")
	if err != nil {
		return 0, 0, fmt.Errorf("AI generation: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
		p.totalAPITokens.Add(int64(usage.TotalAPITokens))
		p.LastStepInputTokens = usage.InputTokens
		p.LastStepOutputTokens = usage.OutputTokens
		p.LastStepCostUSD = usage.CostUSD
	}
	p.LastStepDurationSeconds = time.Since(start).Seconds()

	// Parse AI response.
	result, err := parseTrackResult(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing tracks response: %w", err)
	}

	// 8. Upsert tracks.
	created := 0
	updated := 0

	// Build topic lookup for source_refs.
	topicLookup := make(map[int]db.UnlinkedTopic, len(topics))
	for _, t := range topics {
		topicLookup[t.TopicID] = t
	}

	model := p.cfg.Digest.Model

	for _, nt := range result.NewTracks {
		sourceRefs := p.buildSourceRefs(nt.SourceTopicIDs, topicLookup)
		channelIDs := p.collectChannelIDs(nt.SourceTopicIDs, topicLookup)

		track := db.Track{
			Title:         nt.Title,
			Narrative:     nt.Narrative,
			CurrentStatus: nt.CurrentStatus,
			Participants:  jsonString(nt.Participants),
			Timeline:      jsonString(nt.Timeline),
			KeyMessages:   jsonString(nt.KeyMessages),
			Priority:      validatePriority(nt.Priority),
			Tags:          jsonString(nt.Tags),
			ChannelIDs:    jsonStringArray(channelIDs),
			SourceRefs:    sourceRefs,
			Model:         model,
			PromptVersion: promptVersion,
		}
		if usage != nil {
			track.InputTokens = usage.InputTokens / max(1, len(result.NewTracks)+len(result.UpdatedTracks))
			track.OutputTokens = usage.OutputTokens / max(1, len(result.NewTracks)+len(result.UpdatedTracks))
			track.CostUSD = usage.CostUSD / float64(max(1, len(result.NewTracks)+len(result.UpdatedTracks)))
		}

		id, err := p.db.UpsertTrack(track)
		if err != nil {
			p.logger.Printf("tracks: error inserting new track %q: %v", nt.Title, err)
			continue
		}
		p.logger.Printf("tracks: created track #%d: %s", id, nt.Title)
		created++
	}

	for _, ut := range result.UpdatedTracks {
		if ut.TrackID <= 0 {
			continue
		}
		existing, err := p.db.GetTrackByID(ut.TrackID)
		if err != nil {
			p.logger.Printf("tracks: warning: track %d not found for update: %v", ut.TrackID, err)
			continue
		}

		// Merge new source refs.
		newSourceRefs := p.buildSourceRefs(ut.NewSourceTopicIDs, topicLookup)
		mergedRefs := mergeSourceRefs(existing.SourceRefs, newSourceRefs)

		track := db.Track{
			ID:            ut.TrackID,
			Title:         existing.Title, // title stays
			Narrative:     ut.Narrative,
			CurrentStatus: ut.CurrentStatus,
			Participants:  jsonString(ut.Participants),
			Timeline:      jsonString(ut.Timeline),
			KeyMessages:   jsonString(ut.KeyMessages),
			Priority:      validatePriority(ut.Priority),
			Tags:          jsonString(ut.Tags),
			ChannelIDs:    existing.ChannelIDs,
			SourceRefs:    mergedRefs,
			Model:         model,
			PromptVersion: promptVersion,
		}

		if _, err := p.db.UpsertTrack(track); err != nil {
			p.logger.Printf("tracks: error updating track %d: %v", ut.TrackID, err)
			continue
		}
		p.logger.Printf("tracks: updated track #%d", ut.TrackID)
		updated++
	}

	p.progress(1, 1, fmt.Sprintf("Created %d, updated %d tracks", created, updated))
	p.logger.Printf("tracks: created=%d updated=%d", created, updated)
	return created, updated, nil
}

// FormatActiveTracksForPrompt formats active tracks for injection into rollup prompts.
func (p *Pipeline) FormatActiveTracksForPrompt() (string, error) {
	tracks, err := p.db.GetAllActiveTracks()
	if err != nil {
		return "", fmt.Errorf("loading tracks: %w", err)
	}
	if len(tracks) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, t := range tracks {
		sb.WriteString(fmt.Sprintf("- [track #%d, %s] %s\n  Status: %s\n  Narrative: %s\n",
			t.ID, t.Priority, t.Title, t.CurrentStatus, t.Narrative))
	}
	return sb.String(), nil
}

// --- AI response types ---

type trackResult struct {
	NewTracks     []newTrackResult     `json:"new_tracks"`
	UpdatedTracks []updatedTrackResult `json:"updated_tracks"`
}

type newTrackResult struct {
	Title          string          `json:"title"`
	Narrative      string          `json:"narrative"`
	CurrentStatus  string          `json:"current_status"`
	Participants   json.RawMessage `json:"participants"`
	Timeline       json.RawMessage `json:"timeline"`
	KeyMessages    json.RawMessage `json:"key_messages"`
	Priority       string          `json:"priority"`
	Tags           json.RawMessage `json:"tags"`
	ChannelIDs     json.RawMessage `json:"channel_ids"`
	SourceTopicIDs []int           `json:"source_topic_ids"`
}

type updatedTrackResult struct {
	TrackID           int             `json:"track_id"`
	Narrative         string          `json:"narrative"`
	CurrentStatus     string          `json:"current_status"`
	Participants      json.RawMessage `json:"participants"`
	Timeline          json.RawMessage `json:"timeline"`
	KeyMessages       json.RawMessage `json:"key_messages"`
	Priority          string          `json:"priority"`
	Tags              json.RawMessage `json:"tags"`
	NewSourceTopicIDs []int           `json:"new_source_topic_ids"`
}

func parseTrackResult(raw string) (*trackResult, error) {
	cleaned := cleanJSON(raw)
	var result trackResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing tracks JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

// --- helpers ---

func (p *Pipeline) getPrompt(id string) (string, int) {
	role := ""
	if p.profile != nil {
		role = p.profile.Role
	}
	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.GetForRole(id, role)
		if err == nil {
			roleInstr := prompts.GetRoleInstruction(role)
			if roleInstr != "" {
				tmpl = roleInstr + "\n\n" + tmpl
			}
			return tmpl, version
		}
	}
	tmpl := prompts.Defaults[id]
	roleInstr := prompts.GetRoleInstruction(role)
	if roleInstr != "" {
		tmpl = roleInstr + "\n\n" + tmpl
	}
	return tmpl, 0
}

func (p *Pipeline) loadCaches() {
	channelNames := make(map[string]string)
	userNames := make(map[string]string)

	users, err := p.db.GetUsers(db.UserFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load user names: %v", err)
	} else {
		for _, u := range users {
			name := u.DisplayName
			if name == "" {
				name = u.Name
			}
			userNames[u.ID] = name
		}
	}

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			name := ch.Name
			if name == "" && ch.DMUserID.Valid && ch.DMUserID.String != "" {
				if uname, ok := userNames[ch.DMUserID.String]; ok {
					name = "DM: " + uname
				}
			}
			if name == "" {
				name = ch.ID
			}
			channelNames[ch.ID] = name
		}
	}

	// Load user profile.
	var profile *db.UserProfile
	if uid, err := p.db.GetCurrentUserID(); err == nil && uid != "" {
		profile, _ = p.db.GetUserProfile(uid)
	}

	p.cacheMu.Lock()
	p.channelNames = channelNames
	p.userNames = userNames
	p.profile = profile
	p.cacheMu.Unlock()
}

func (p *Pipeline) channelName(id string) string {
	p.cacheMu.RLock()
	name, ok := p.channelNames[id]
	p.cacheMu.RUnlock()
	if ok {
		return sanitize(name)
	}
	return id
}

func (p *Pipeline) progress(done, total int, status string) {
	if p.OnProgress != nil {
		p.OnProgress(done, total, status)
	}
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Write ALL text values in %s.", lang)
	}
	return "Write in the language most commonly used in the messages."
}

func (p *Pipeline) formatExistingTracks(tracks []db.Track) string {
	if len(tracks) == 0 {
		return "(no existing tracks)"
	}
	var sb strings.Builder
	for _, t := range tracks {
		sb.WriteString(fmt.Sprintf("Track #%d: %s [%s]\n  Status: %s\n  Narrative: %s\n\n",
			t.ID, sanitize(t.Title), t.Priority, sanitize(t.CurrentStatus), sanitize(t.Narrative)))
	}
	return sb.String()
}

func (p *Pipeline) formatUnlinkedTopics(topics []db.UnlinkedTopic) string {
	if len(topics) == 0 {
		return "(no unlinked topics)"
	}
	var sb strings.Builder
	for _, t := range topics {
		chName := t.ChannelName
		if chName == "" {
			chName = t.ChannelID
		}
		sb.WriteString(fmt.Sprintf("Topic [topic_id=%d, digest_id=%d, #%s]: %s\n  %s\n",
			t.TopicID, t.DigestID, sanitize(chName), sanitize(t.Title), sanitize(t.Summary)))
		if t.Decisions != "" && t.Decisions != "[]" {
			sb.WriteString(fmt.Sprintf("  Decisions: %s\n", t.Decisions))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (p *Pipeline) loadChannelSummaries(topics []db.UnlinkedTopic) string {
	// Collect unique channel IDs from topics.
	seen := make(map[string]bool)
	var channelIDs []string
	for _, t := range topics {
		if !seen[t.ChannelID] {
			seen[t.ChannelID] = true
			channelIDs = append(channelIDs, t.ChannelID)
		}
	}

	var sb strings.Builder
	for _, chID := range channelIDs {
		result, err := p.db.GetLatestRunningSummaryWithAge(chID, "channel")
		if err != nil || result == nil || result.Summary == "" {
			continue
		}
		age := time.Duration(result.AgeDays*24) * time.Hour
		// TTL: 7 days mark outdated, 30 days skip.
		if age > 30*24*time.Hour {
			continue
		}
		chName := p.channelName(chID)
		if age > 7*24*time.Hour {
			sb.WriteString(fmt.Sprintf("=== #%s CHANNEL CONTEXT (outdated) ===\n%s\n\n", chName, result.Summary))
		} else {
			sb.WriteString(fmt.Sprintf("=== #%s CHANNEL CONTEXT ===\n%s\n\n", chName, result.Summary))
		}
	}
	return sb.String()
}

func (p *Pipeline) buildSourceRefs(topicIDs []int, lookup map[int]db.UnlinkedTopic) string {
	type sourceRef struct {
		DigestID  int     `json:"digest_id"`
		TopicID   int     `json:"topic_id"`
		ChannelID string  `json:"channel_id"`
		Timestamp float64 `json:"timestamp"`
	}
	var refs []sourceRef
	for _, tid := range topicIDs {
		if t, ok := lookup[tid]; ok {
			refs = append(refs, sourceRef{
				DigestID:  t.DigestID,
				TopicID:   t.TopicID,
				ChannelID: t.ChannelID,
				Timestamp: t.PeriodTo,
			})
		}
	}
	if len(refs) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(refs)
	return string(data)
}

func (p *Pipeline) collectChannelIDs(topicIDs []int, lookup map[int]db.UnlinkedTopic) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, tid := range topicIDs {
		if t, ok := lookup[tid]; ok {
			if !seen[t.ChannelID] {
				seen[t.ChannelID] = true
				ids = append(ids, t.ChannelID)
			}
		}
	}
	return ids
}

func mergeSourceRefs(existingJSON, newJSON string) string {
	type ref struct {
		DigestID  int     `json:"digest_id"`
		TopicID   int     `json:"topic_id"`
		ChannelID string  `json:"channel_id"`
		Timestamp float64 `json:"timestamp"`
	}
	var existing, newRefs []ref
	_ = json.Unmarshal([]byte(existingJSON), &existing)
	_ = json.Unmarshal([]byte(newJSON), &newRefs)

	type key struct {
		digestID int
		topicID  int
	}
	seen := make(map[key]bool)
	for _, r := range existing {
		seen[key{r.DigestID, r.TopicID}] = true
	}
	for _, r := range newRefs {
		k := key{r.DigestID, r.TopicID}
		if !seen[k] {
			seen[k] = true
			existing = append(existing, r)
		}
	}
	data, _ := json.Marshal(existing)
	return string(data)
}

func validatePriority(p string) string {
	switch p {
	case "high", "medium", "low":
		return p
	default:
		return "medium"
	}
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	return string(raw)
}

func jsonStringArray(arr []string) string {
	if len(arr) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(arr)
	return string(data)
}

func sanitize(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "```", "` ` `")
	text = strings.ReplaceAll(text, "===", "= = =")
	text = strings.ReplaceAll(text, "---", "- - -")
	return text
}

// cleanJSON strips markdown fences and trims to the outermost JSON braces.
func cleanJSON(raw string) string {
	cleaned := raw
	if idx := strings.Index(raw, "```json"); idx >= 0 {
		cleaned = raw[idx+7:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	} else if idx := strings.Index(raw, "```"); idx >= 0 {
		cleaned = raw[idx+3:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	}

	cleaned = strings.TrimSpace(cleaned)
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}
	return cleaned
}
