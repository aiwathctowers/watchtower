package ai

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QueryIntent represents the detected intent of a user query.
type QueryIntent int

const (
	IntentGeneral QueryIntent = iota
	IntentCatchup
	IntentSearch
	IntentPerson
	IntentChannel
)

// String returns a human-readable label for a QueryIntent.
func (i QueryIntent) String() string {
	switch i {
	case IntentCatchup:
		return "catchup"
	case IntentSearch:
		return "search"
	case IntentPerson:
		return "person"
	case IntentChannel:
		return "channel"
	default:
		return "general"
	}
}

// TimeRange represents a parsed time range.
type TimeRange struct {
	From time.Time
	To   time.Time
}

// ParsedQuery holds the result of parsing a user's natural language question.
type ParsedQuery struct {
	RawText     string
	TimeRange   *TimeRange
	Channels    []string
	Users       []string
	Topics      []string
	Intent      QueryIntent
	WatchedOnly bool // only include watched channels/users in context
}

// Parse deterministically extracts structured information from a query string.
// It does not make any AI calls. Uses the current time for relative time ranges.
// For testing, use ParseAt to provide a fixed time.
func Parse(input string) ParsedQuery {
	return ParseAt(input, time.Now())
}

// ParseAt is like Parse but uses the provided time for resolving relative time
// expressions (e.g. "yesterday", "last 2 hours"). This is safe for concurrent
// use and testing without mutating global state.
func ParseAt(input string, now time.Time) ParsedQuery {
	pq := ParsedQuery{
		RawText: input,
	}
	remaining := input

	remaining = extractTimeRange(&pq, remaining, now)
	remaining = extractChannels(&pq, remaining)
	remaining = extractUsers(&pq, remaining)
	detectIntent(&pq, input)
	extractTopics(&pq, remaining)

	return pq
}

// --- Channel extraction ---

var channelLiteralRe = regexp.MustCompile(`#([\w-]+)`)
var channelFuzzyRe = regexp.MustCompile(`(?i)\b(?:in|from)\s+([\w-]+)\s+channel\b`)
var inChannelRe = regexp.MustCompile(`(?i)\bin\s+([\w-]+)\b`)

func extractChannels(pq *ParsedQuery, text string) string {
	// Literal #channel-name
	for _, m := range channelLiteralRe.FindAllStringSubmatch(text, -1) {
		pq.Channels = append(pq.Channels, m[1])
	}
	text = channelLiteralRe.ReplaceAllString(text, "")

	// "in <name> channel"
	for _, m := range channelFuzzyRe.FindAllStringSubmatch(text, -1) {
		pq.Channels = append(pq.Channels, m[1])
	}
	text = channelFuzzyRe.ReplaceAllString(text, "")

	// "summarize #X" pattern is handled by channel literal above.
	// "in <name>" without "channel" — more generic pattern
	for _, m := range inChannelRe.FindAllStringSubmatch(text, -1) {
		word := strings.ToLower(m[1])
		if isStopWord(word) {
			continue
		}
		pq.Channels = append(pq.Channels, m[1])
	}
	text = removeMatchedNonStopWords(text, inChannelRe)

	pq.Channels = dedup(pq.Channels)
	return text
}

// removeMatchedNonStopWords removes regex matches whose first capture group is
// not a stop word. Matches on stop words are left intact.
func removeMatchedNonStopWords(text string, re *regexp.Regexp) string {
	return re.ReplaceAllStringFunc(text, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) > 1 && isStopWord(strings.ToLower(sub[1])) {
			return match
		}
		return ""
	})
}

// --- User extraction ---

var userLiteralRe = regexp.MustCompile(`@([\w.-]+)`)
var userFromRe = regexp.MustCompile(`(?i)\b(?:from|by)\s+([\w.-]+)\b`)
var userSaidRe = regexp.MustCompile(`(?i)\b([\w.-]+)\s+said\b`)
var userWhatDidRe = regexp.MustCompile(`(?i)\bwhat\s+did\s+([\w.-]+)\b`)
var punctuationRe = regexp.MustCompile(`[^\w\s-]`)

func extractUsers(pq *ParsedQuery, text string) string {
	// Literal @username
	for _, m := range userLiteralRe.FindAllStringSubmatch(text, -1) {
		pq.Users = append(pq.Users, m[1])
	}
	text = userLiteralRe.ReplaceAllString(text, "")

	// "from alice" / "by alice"
	for _, m := range userFromRe.FindAllStringSubmatch(text, -1) {
		word := strings.ToLower(m[1])
		if isStopWord(word) {
			continue
		}
		pq.Users = append(pq.Users, m[1])
	}
	text = removeMatchedNonStopWords(text, userFromRe)

	// "alice said"
	for _, m := range userSaidRe.FindAllStringSubmatch(text, -1) {
		word := strings.ToLower(m[1])
		if isStopWord(word) {
			continue
		}
		pq.Users = append(pq.Users, m[1])
	}
	text = removeMatchedNonStopWords(text, userSaidRe)

	// "what did alice"
	for _, m := range userWhatDidRe.FindAllStringSubmatch(text, -1) {
		word := strings.ToLower(m[1])
		if isStopWord(word) {
			continue
		}
		pq.Users = append(pq.Users, m[1])
	}
	text = removeMatchedNonStopWords(text, userWhatDidRe)

	pq.Users = dedup(pq.Users)
	return text
}

// --- Time range extraction ---

var (
	relDurationRe  = regexp.MustCompile(`(?i)\b(?:last|past)\s+(\d+)\s*(h(?:ours?)?|m(?:in(?:utes?)?)?|d(?:ays?)?|w(?:eeks?)?)\b`)
	sinceWeekdayRe = regexp.MustCompile(`(?i)\bsince\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
	yesterdayRe    = regexp.MustCompile(`(?i)\byesterday\b`)
	todayRe        = regexp.MustCompile(`(?i)\btoday\b`)
	thisMorningRe  = regexp.MustCompile(`(?i)\bthis\s+morning\b`)
	lastWeekRe     = regexp.MustCompile(`(?i)\blast\s+week\b`)
)

func extractTimeRange(pq *ParsedQuery, text string, now time.Time) string {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	lower := strings.ToLower(text)

	// "yesterday"
	if strings.Contains(lower, "yesterday") {
		yStart := todayStart.AddDate(0, 0, -1)
		yEnd := time.Date(yStart.Year(), yStart.Month(), yStart.Day(), 23, 59, 59, 0, now.Location())
		pq.TimeRange = &TimeRange{From: yStart, To: yEnd}
		return removeWord(text, yesterdayRe)
	}

	// "today"
	if strings.Contains(lower, "today") {
		pq.TimeRange = &TimeRange{From: todayStart, To: now}
		return removeWord(text, todayRe)
	}

	// "this morning"
	if strings.Contains(lower, "this morning") {
		morning := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())
		noon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
		from := morning
		if now.Before(morning) {
			from = todayStart
		}
		to := noon
		if now.Before(noon) {
			to = now
		}
		pq.TimeRange = &TimeRange{From: from, To: to}
		return removeWord(text, thisMorningRe)
	}

	// "last week"
	if strings.Contains(lower, "last week") {
		weekday := int(todayStart.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		thisMonday := todayStart.AddDate(0, 0, -(weekday - 1))
		prevMonday := thisMonday.AddDate(0, 0, -7)
		prevSunday := time.Date(thisMonday.Year(), thisMonday.Month(), thisMonday.Day()-1, 23, 59, 59, 0, now.Location())
		pq.TimeRange = &TimeRange{From: prevMonday, To: prevSunday}
		return removeWord(text, lastWeekRe)
	}

	// "last N h/m/d/w" or "past N h/m/d/w"
	if m := relDurationRe.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := strings.ToLower(m[2])
		var d time.Duration
		switch {
		case strings.HasPrefix(unit, "h"):
			d = time.Duration(n) * time.Hour
		case strings.HasPrefix(unit, "m"):
			d = time.Duration(n) * time.Minute
		case strings.HasPrefix(unit, "d"):
			d = time.Duration(n) * 24 * time.Hour
		case strings.HasPrefix(unit, "w"):
			d = time.Duration(n) * 7 * 24 * time.Hour
		}
		pq.TimeRange = &TimeRange{From: now.Add(-d), To: now}
		text = relDurationRe.ReplaceAllString(text, "")
		return text
	}

	// "since Monday"
	if m := sinceWeekdayRe.FindStringSubmatch(text); m != nil {
		target := parseWeekday(m[1])
		dayStart := mostRecentWeekday(todayStart, target)
		pq.TimeRange = &TimeRange{From: dayStart, To: now}
		text = sinceWeekdayRe.ReplaceAllString(text, "")
		return text
	}

	return text
}

func parseWeekday(s string) time.Weekday {
	switch strings.ToLower(s) {
	case "sunday":
		return time.Sunday
	case "monday":
		return time.Monday
	case "tuesday":
		return time.Tuesday
	case "wednesday":
		return time.Wednesday
	case "thursday":
		return time.Thursday
	case "friday":
		return time.Friday
	case "saturday":
		return time.Saturday
	}
	return time.Monday
}

// mostRecentWeekday returns the most recent occurrence of the given weekday
// at 00:00. If today is that weekday, it returns today's start.
func mostRecentWeekday(todayStart time.Time, target time.Weekday) time.Time {
	current := todayStart.Weekday()
	diff := (int(current) - int(target) + 7) % 7
	if diff == 0 {
		return todayStart
	}
	return todayStart.AddDate(0, 0, -diff)
}

func removeWord(text string, re *regexp.Regexp) string {
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// --- Intent detection ---

var (
	catchupPatterns = compilePatterns([]string{
		`(?i)\bwhat(?:'s| is| has)?\s+(?:happened|new|going on|up)\b`,
		`(?i)\bcatch\s*(?:me\s+)?up\b`,
		`(?i)\bsummar(?:y|ize|ise)\b`,
		`(?i)\bupdate\s+me\b`,
		`(?i)\bbring\s+me\s+up\s+to\s+(?:speed|date)\b`,
		`(?i)\bwhat\s+did\s+i\s+miss\b`,
	})
	searchPatterns = compilePatterns([]string{
		`(?i)\bfind\s+(?:messages?|conversations?)\s+about\b`,
		`(?i)\bsearch\s+(?:for|messages?)?\b`,
		`(?i)\blook\s+(?:for|up)\b`,
		`(?i)\bshow\s+me\s+messages?\b`,
	})
	personPatterns = compilePatterns([]string{
		`(?i)\bwhat\s+did\s+\S+\s+say\b`,
		`(?i)\bwhat\s+has\s+\S+\s+(?:been|said|posted|mentioned)\b`,
		`(?i)\b\S+(?:'s| is)\s+(?:messages?|activity|posts?)\b`,
	})
	channelPatterns = compilePatterns([]string{
		`(?i)\bsummar(?:y|ize|ise)\s+#`,
		`(?i)\bwhat(?:'s| is)\s+(?:happening|going on)\s+in\b`,
		`(?i)\b(?:activity|discussion|updates?)\s+in\b`,
	})
)

func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		compiled[i] = regexp.MustCompile(p)
	}
	return compiled
}

func detectIntent(pq *ParsedQuery, text string) {
	// Check in order of specificity
	if matchesAny(text, personPatterns) && len(pq.Users) > 0 {
		pq.Intent = IntentPerson
		return
	}
	if matchesAny(text, channelPatterns) && len(pq.Channels) > 0 {
		pq.Intent = IntentChannel
		return
	}
	if matchesAny(text, catchupPatterns) {
		pq.Intent = IntentCatchup
		return
	}
	if matchesAny(text, searchPatterns) {
		pq.Intent = IntentSearch
		return
	}
	pq.Intent = IntentGeneral
}

func matchesAny(text string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// --- Topic extraction ---

// stopWords are common words that should not become FTS5 search topics.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "shall": true, "can": true, "to": true, "of": true,
	"in": true, "for": true, "on": true, "with": true, "at": true,
	"by": true, "from": true, "about": true, "as": true, "into": true,
	"through": true, "during": true, "before": true, "after": true,
	"above": true, "below": true, "between": true, "and": true, "but": true,
	"or": true, "nor": true, "not": true, "so": true, "yet": true,
	"both": true, "either": true, "neither": true, "each": true, "every": true,
	"all": true, "any": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "no": true, "only": true,
	"own": true, "same": true, "than": true, "too": true, "very": true,
	"just": true, "because": true, "that": true, "this": true, "these": true,
	"those": true, "what": true, "which": true, "who": true, "whom": true,
	"how": true, "when": true, "where": true, "why": true, "i": true,
	"me": true, "my": true, "we": true, "us": true, "our": true,
	"you": true, "your": true, "he": true, "him": true, "his": true,
	"she": true, "her": true, "it": true, "its": true, "they": true,
	"them": true, "their": true, "there": true, "here": true,
	"what's": true, "happening": true, "happened": true, "going": true,
	"new": true, "up": true, "tell": true, "show": true, "find": true,
	"search": true, "look": true, "get": true, "give": true, "let": true,
	"said": true, "say": true, "talk": true, "talking": true, "talked": true,
	"discuss": true, "discussed": true, "discussion": true,
	"messages": true, "message": true, "channel": true,
	"summarize": true, "summarise": true, "summary": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

func extractTopics(pq *ParsedQuery, text string) {
	// Remove punctuation except hyphens within words
	cleaned := punctuationRe.ReplaceAllString(text, " ")
	words := strings.Fields(cleaned)
	var topics []string
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) < 2 {
			continue
		}
		if isStopWord(lower) {
			continue
		}
		topics = append(topics, lower)
	}
	pq.Topics = dedup(topics)
}

// --- Helpers ---

func dedup(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	var result []string
	for _, s := range ss {
		lower := strings.ToLower(s)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		result = append(result, s)
	}
	return result
}
