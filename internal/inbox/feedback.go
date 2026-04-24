package inbox

import (
	"context"
	"fmt"
	"log"

	"watchtower/internal/db"
)

// SubmitFeedback writes a feedback row and updates learned rules based on rating/reason.
// rating: -1 negative, +1 positive. reason: one of source_noise, wrong_priority, wrong_class, never_show, ”.
// An optional logger may be passed as the last argument; if omitted, no rule-update log is emitted.
func SubmitFeedback(ctx context.Context, database *db.DB, itemID int64, rating int, reason string, logger ...*log.Logger) error {
	// 1. Write raw feedback row first.
	if err := database.RecordInboxFeedback(itemID, rating, reason); err != nil {
		return fmt.Errorf("record feedback: %w", err)
	}

	// 2. Load the item to know sender/channel (separate query, no open cursor).
	item, err := database.GetInboxItem(itemID)
	if err != nil {
		return fmt.Errorf("get item: %w", err)
	}

	// 3. Apply rule updates and item class adjustments based on rating+reason.
	var ruleType, scopeKey string
	var ruleWeight float64
	switch {
	case rating == -1 && reason == "never_show":
		ruleType, scopeKey, ruleWeight = "source_mute", "sender:"+item.SenderUserID, -1.0
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      scopeKey,
			Weight:        ruleWeight,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "source_noise":
		ruleType, scopeKey, ruleWeight = "source_mute", "sender:"+item.SenderUserID, -0.8
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      scopeKey,
			Weight:        ruleWeight,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "wrong_class":
		if item.ItemClass == "actionable" {
			_ = database.SetInboxItemClass(itemID, "ambient")
		}
		ruleType, scopeKey, ruleWeight = "trigger_downgrade", "trigger:"+item.TriggerType+":sender:"+item.SenderUserID, -0.6
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      scopeKey,
			Weight:        ruleWeight,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "wrong_priority":
		ruleType, scopeKey, ruleWeight = "trigger_downgrade", "sender:"+item.SenderUserID, -0.5
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      scopeKey,
			Weight:        ruleWeight,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == 1:
		ruleType, scopeKey, ruleWeight = "source_boost", "sender:"+item.SenderUserID, 0.6
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      scopeKey,
			Weight:        ruleWeight,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	}

	if ruleType != "" && len(logger) > 0 && logger[0] != nil {
		logger[0].Printf("inbox_feedback: item=%d rating=%+d reason=%s → rule %s %s weight=%.1f",
			itemID, rating, reason, ruleType, scopeKey, ruleWeight)
	}

	return nil
}
