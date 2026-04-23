package inbox

import (
	"context"
	"fmt"

	"watchtower/internal/db"
)

// SubmitFeedback writes a feedback row and updates learned rules based on rating/reason.
// rating: -1 negative, +1 positive. reason: one of source_noise, wrong_priority, wrong_class, never_show, ''.
func SubmitFeedback(ctx context.Context, database *db.DB, itemID int64, rating int, reason string) error {
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
	switch {
	case rating == -1 && reason == "never_show":
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "source_mute",
			ScopeKey:      "sender:" + item.SenderUserID,
			Weight:        -1.0,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "source_noise":
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "source_mute",
			ScopeKey:      "sender:" + item.SenderUserID,
			Weight:        -0.8,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "wrong_class":
		if item.ItemClass == "actionable" {
			_ = database.SetInboxItemClass(itemID, "ambient")
		}
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "trigger_downgrade",
			ScopeKey:      "trigger:" + item.TriggerType + ":sender:" + item.SenderUserID,
			Weight:        -0.6,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == -1 && reason == "wrong_priority":
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "trigger_downgrade",
			ScopeKey:      "sender:" + item.SenderUserID,
			Weight:        -0.5,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	case rating == 1:
		_ = database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "source_boost",
			ScopeKey:      "sender:" + item.SenderUserID,
			Weight:        0.6,
			Source:        "explicit_feedback",
			EvidenceCount: 1,
		})
	}
	return nil
}
