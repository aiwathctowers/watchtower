package inbox

import "testing"

func TestClassifier_DefaultForTriggerType(t *testing.T) {
	cases := []struct {
		trig  string
		class string
	}{
		{"mention", "actionable"},
		{"dm", "actionable"},
		{"thread_reply", "actionable"},
		{"reaction", "ambient"},
		{"jira_assigned", "actionable"},
		{"jira_comment_mention", "actionable"},
		{"jira_comment_watching", "ambient"},
		{"jira_status_change", "ambient"},
		{"jira_priority_change", "ambient"},
		{"calendar_invite", "actionable"},
		{"calendar_time_change", "actionable"},
		{"calendar_cancelled", "ambient"},
		{"decision_made", "ambient"},
		{"briefing_ready", "ambient"},
		{"unknown_type", "ambient"}, // unknown → ambient fallback
	}
	for _, c := range cases {
		got := DefaultItemClass(c.trig)
		if got != c.class {
			t.Errorf("trigger=%s got=%s want=%s", c.trig, got, c.class)
		}
	}
}

func TestClassifier_ApplyAIOverride_DowngradeOnly(t *testing.T) {
	// AI can downgrade actionable → ambient
	got := ApplyAIOverride("actionable", "ambient")
	if got != "ambient" {
		t.Errorf("downgrade failed: %s", got)
	}

	// AI cannot upgrade ambient → actionable
	got = ApplyAIOverride("ambient", "actionable")
	if got != "ambient" {
		t.Errorf("upgrade should be rejected: %s", got)
	}

	// Empty override keeps original
	got = ApplyAIOverride("actionable", "")
	if got != "actionable" {
		t.Errorf("empty should keep: %s", got)
	}
}
