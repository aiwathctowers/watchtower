package inbox

var defaultClasses = map[string]string{
	"mention":               "actionable",
	"dm":                    "actionable",
	"thread_reply":          "actionable",
	"reaction":              "ambient",
	"jira_assigned":         "actionable",
	"jira_comment_mention":  "actionable",
	"jira_comment_watching": "ambient",
	"jira_status_change":    "ambient",
	"jira_priority_change":  "ambient",
	"calendar_invite":       "actionable",
	"calendar_time_change":  "actionable",
	"calendar_cancelled":    "ambient",
	"decision_made":         "ambient",
	"briefing_ready":        "ambient",
}

// DefaultItemClass returns 'actionable' or 'ambient' for a known trigger type, defaulting to 'ambient' for unknown.
func DefaultItemClass(trig string) string {
	if c, ok := defaultClasses[trig]; ok {
		return c
	}
	return "ambient"
}

// ApplyAIOverride applies an AI-suggested class override.
// Only downgrades (actionable → ambient) are honored; upgrades are silently rejected.
// Empty override returns the original class.
func ApplyAIOverride(current, override string) string {
	if override == "" {
		return current
	}
	if current == "actionable" && override == "ambient" {
		return "ambient"
	}
	return current
}
