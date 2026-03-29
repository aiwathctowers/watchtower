package sync

import (
	"context"
)

// syncThread fetches all replies for a single thread and upserts them.
// Used inline during full sync (message_sync.go) when sync_threads is enabled.
// Returns the number of replies synced and any error.
func (o *Orchestrator) syncThread(ctx context.Context, channelID, threadTS string) (int, error) {
	replies, err := o.slackClient.GetConversationReplies(ctx, channelID, threadTS)
	if err != nil {
		return 0, err
	}

	if len(replies) == 0 {
		return 0, nil
	}

	return o.upsertMessagePage(channelID, replies)
}
