package slack

import (
	"context"
	"errors"
	"log"

	"github.com/slack-go/slack"
)

// Client wraps the slack-go client with rate limiting.
type Client struct {
	api         *slack.Client
	rateLimiter *RateLimiter
	logger      *log.Logger
}

// NewClient creates a new rate-limited Slack client.
func NewClient(token string) *Client {
	return &Client{
		api:         slack.New(token),
		rateLimiter: NewRateLimiter(),
	}
}

// NewClientWithAPI creates a client with a pre-configured slack.Client
// (useful for testing with custom HTTP endpoints).
func NewClientWithAPI(api *slack.Client) *Client {
	return &Client{
		api:         api,
		rateLimiter: NewRateLimiter(),
	}
}

// NewClientWithAPIUnlimited creates a client with a pre-configured slack.Client
// and an unlimited rate limiter (for testing).
func NewClientWithAPIUnlimited(api *slack.Client) *Client {
	return &Client{
		api:         api,
		rateLimiter: NewUnlimitedRateLimiter(),
	}
}

// SetLogger sets a logger for the client and its rate limiter.
func (c *Client) SetLogger(l *log.Logger) {
	c.logger = l
	c.rateLimiter.logger = l
}

func (c *Client) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}

// doRequest acquires the gate, calls fn, handles errors, and releases the gate.
// Rate limit (429) errors are retried automatically after the backoff period.
func (c *Client) doRequest(ctx context.Context, tier int, fn func() error) error {
	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := c.rateLimiter.Wait(ctx, tier); err != nil {
			return err
		}
		err := fn()
		c.handleError(tier, err) // set backoff BEFORE releasing gate
		c.rateLimiter.Done(tier)

		if err == nil {
			return nil
		}

		// Retry on rate limit errors; handleError already set the backoff.
		var rlErr *slack.RateLimitedError
		if errors.As(err, &rlErr) && attempt < maxRetries {
			c.logf("rate limited, retrying (attempt %d/%d)", attempt+1, maxRetries)
			continue
		}
		return err
	}
	return nil // unreachable
}

// AuthTestResponse holds the result of an auth.test API call.
type AuthTestResponse struct {
	UserID string
	User   string // username
	TeamID string
}

// AuthTest calls the auth.test API to identify the token owner. (Tier 2)
func (c *Client) AuthTest(ctx context.Context) (*AuthTestResponse, error) {
	var resp *slack.AuthTestResponse
	err := c.doRequest(ctx, Tier2, func() error {
		c.logf("slack API: auth.test")
		var err error
		resp, err = c.api.AuthTestContext(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &AuthTestResponse{
		UserID: resp.UserID,
		User:   resp.User,
		TeamID: resp.TeamID,
	}, nil
}

// GetTeamInfo returns workspace information. (Tier 2)
func (c *Client) GetTeamInfo(ctx context.Context) (*slack.TeamInfo, error) {
	var info *slack.TeamInfo
	err := c.doRequest(ctx, Tier2, func() error {
		c.logf("slack API: team.info")
		var err error
		info, err = c.api.GetTeamInfoContext(ctx)
		return err
	})
	return info, err
}

// GetUsers returns all users in the workspace, handling pagination. (Tier 2)
// The optional onPage callback is called after each page with the running total.
func (c *Client) GetUsers(ctx context.Context, onPage ...func(fetched int)) ([]slack.User, error) {
	var allUsers []slack.User

	p := c.api.GetUsersPaginated(slack.GetUsersOptionLimit(200))
	for {
		var done bool
		err := c.doRequest(ctx, Tier2, func() error {
			c.logf("slack API: users.list (fetched so far: %d)", len(allUsers))
			var err error
			p, err = p.Next(ctx)
			if err != nil && p.Done(err) {
				done = true
				return nil
			}
			return err
		})
		if err != nil {
			return allUsers, err
		}
		if done {
			break
		}
		allUsers = append(allUsers, p.Users...)
		if len(onPage) > 0 && onPage[0] != nil {
			onPage[0](len(allUsers))
		}
	}

	return allUsers, nil
}

// GetChannels returns channels of the specified types, handling pagination. (Tier 2)
// If types is empty, fetches public and private channels only.
// The optional onPage callback is called after each page with the running total.
func (c *Client) GetChannels(ctx context.Context, types []string, onPage ...func(fetched int)) ([]slack.Channel, error) {
	if len(types) == 0 {
		types = []string{"public_channel", "private_channel"}
	}
	var allChannels []slack.Channel
	cursor := ""

	for {
		var channels []slack.Channel
		var nextCursor string
		err := c.doRequest(ctx, Tier2, func() error {
			c.logf("slack API: conversations.list (fetched so far: %d)", len(allChannels))
			var err error
			channels, nextCursor, err = c.api.GetConversationsContext(ctx, &slack.GetConversationsParameters{
				Types:  types,
				Limit:  200,
				Cursor: cursor,
			})
			return err
		})
		if err != nil {
			return allChannels, err
		}

		allChannels = append(allChannels, channels...)
		if len(onPage) > 0 && onPage[0] != nil {
			onPage[0](len(allChannels))
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

// HistoryOptions configures a conversation history request.
type HistoryOptions struct {
	ChannelID string
	Cursor    string
	Oldest    string
	Latest    string
	Limit     int
}

// HistoryResponse contains messages and pagination info from conversation history.
type HistoryResponse struct {
	Messages   []slack.Message
	HasMore    bool
	NextCursor string
}

// GetConversationHistory fetches one page of message history. (Tier 3)
func (c *Client) GetConversationHistory(ctx context.Context, opts HistoryOptions) (*HistoryResponse, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 200
	}

	var resp *slack.GetConversationHistoryResponse
	err := c.doRequest(ctx, Tier3, func() error {
		c.logf("slack API: conversations.history channel=%s oldest=%s cursor=%q", opts.ChannelID, opts.Oldest, opts.Cursor)
		var err error
		resp, err = c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: opts.ChannelID,
			Cursor:    opts.Cursor,
			Oldest:    opts.Oldest,
			Latest:    opts.Latest,
			Limit:     limit,
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return &HistoryResponse{
		Messages:   resp.Messages,
		HasMore:    resp.HasMore,
		NextCursor: resp.ResponseMetaData.NextCursor,
	}, nil
}

// GetConversationReplies fetches all replies for a thread, handling pagination. (Tier 3)
func (c *Client) GetConversationReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error) {
	var allMessages []slack.Message
	cursor := ""

	for {
		var msgs []slack.Message
		var hasMore bool
		var nextCursor string

		err := c.doRequest(ctx, Tier3, func() error {
			c.logf("slack API: conversations.replies channel=%s thread=%s", channelID, threadTS)
			var err error
			msgs, hasMore, nextCursor, err = c.api.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
				ChannelID: channelID,
				Timestamp: threadTS,
				Cursor:    cursor,
				Limit:     200,
			})
			return err
		})
		if err != nil {
			return allMessages, err
		}

		allMessages = append(allMessages, msgs...)

		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allMessages, nil
}

// SearchResult holds one page of search.messages results.
type SearchResult struct {
	Messages []slack.SearchMessage
	Total    int
	Page     int
	Pages    int
}

// SearchMessages searches for messages matching the query. (Tier 2)
// Returns one page of results; use page parameter for pagination (1-indexed).
func (c *Client) SearchMessages(ctx context.Context, query string, page int) (*SearchResult, error) {
	var result *slack.SearchMessages
	err := c.doRequest(ctx, Tier2, func() error {
		c.logf("slack API: search.messages query=%q page=%d", query, page)
		var err error
		result, err = c.api.SearchMessagesContext(ctx, query, slack.SearchParameters{
			Sort:          "timestamp",
			SortDirection: "desc",
			Count:         100,
			Page:          page,
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return &SearchResult{
		Messages: result.Matches,
		Total:    result.Total,
		Page:     result.Paging.Page,
		Pages:    result.Paging.Pages,
	}, nil
}

// GetUserInfo fetches a single user's profile. (Tier 4)
func (c *Client) GetUserInfo(ctx context.Context, userID string) (*slack.User, error) {
	var user *slack.User
	err := c.doRequest(ctx, Tier4, func() error {
		c.logf("slack API: users.info user=%s", userID)
		var err error
		user, err = c.api.GetUserInfoContext(ctx, userID)
		return err
	})
	return user, err
}

// GetEmoji fetches all custom emojis in the workspace. (Tier 2)
// Returns a map of emoji name → URL (or "alias:other_name" for aliases).
func (c *Client) GetEmoji(ctx context.Context) (map[string]string, error) {
	var emojis map[string]string
	err := c.doRequest(ctx, Tier2, func() error {
		c.logf("slack API: emoji.list")
		var err error
		emojis, err = c.api.GetEmojiContext(ctx)
		return err
	})
	return emojis, err
}

// APIStats returns per-tier request counts and total 429 retry count.
func (c *Client) APIStats() (counts map[int]int, retries int) {
	return c.rateLimiter.Stats()
}

// ResetAPIStats clears all request counters.
func (c *Client) ResetAPIStats() {
	c.rateLimiter.ResetStats()
}

// handleError checks for rate limit errors and sets backoff accordingly.
func (c *Client) handleError(tier int, err error) {
	if err == nil {
		return
	}
	var rlErr *slack.RateLimitedError
	if errors.As(err, &rlErr) {
		c.rateLimiter.HandleRateLimit(tier, rlErr.RetryAfter)
	}
}
