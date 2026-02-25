package slack

import (
	"context"
	"errors"

	"github.com/slack-go/slack"
)

// Client wraps the slack-go client with rate limiting.
type Client struct {
	api         *slack.Client
	rateLimiter *RateLimiter
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

// GetTeamInfo returns workspace information. (Tier 2)
func (c *Client) GetTeamInfo(ctx context.Context) (*slack.TeamInfo, error) {
	if err := c.rateLimiter.Wait(ctx, Tier2); err != nil {
		return nil, err
	}
	info, err := c.api.GetTeamInfoContext(ctx)
	if err != nil {
		c.handleError(Tier2, err)
		return nil, err
	}
	return info, nil
}

// GetUsers returns all users in the workspace, handling pagination. (Tier 2)
func (c *Client) GetUsers(ctx context.Context) ([]slack.User, error) {
	var allUsers []slack.User

	p := c.api.GetUsersPaginated(slack.GetUsersOptionLimit(200))
	for {
		if err := c.rateLimiter.Wait(ctx, Tier2); err != nil {
			return allUsers, err
		}

		var err error
		p, err = p.Next(ctx)
		if p.Done(err) {
			break
		}
		if err != nil {
			c.handleError(Tier2, err)
			return allUsers, err
		}

		allUsers = append(allUsers, p.Users...)
	}

	return allUsers, nil
}

// GetChannels returns all channels of all types, handling pagination. (Tier 2)
func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		if err := c.rateLimiter.Wait(ctx, Tier2); err != nil {
			return allChannels, err
		}

		params := &slack.GetConversationsParameters{
			Types:  []string{"public_channel", "private_channel", "im", "mpim"},
			Limit:  200,
			Cursor: cursor,
		}

		channels, nextCursor, err := c.api.GetConversationsContext(ctx, params)
		if err != nil {
			c.handleError(Tier2, err)
			return allChannels, err
		}

		allChannels = append(allChannels, channels...)

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
	if err := c.rateLimiter.Wait(ctx, Tier3); err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 200
	}

	params := &slack.GetConversationHistoryParameters{
		ChannelID: opts.ChannelID,
		Cursor:    opts.Cursor,
		Oldest:    opts.Oldest,
		Latest:    opts.Latest,
		Limit:     limit,
	}

	resp, err := c.api.GetConversationHistoryContext(ctx, params)
	if err != nil {
		c.handleError(Tier3, err)
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
		if err := c.rateLimiter.Wait(ctx, Tier3); err != nil {
			return allMessages, err
		}

		params := &slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
			Limit:     200,
		}

		msgs, hasMore, nextCursor, err := c.api.GetConversationRepliesContext(ctx, params)
		if err != nil {
			c.handleError(Tier3, err)
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

// handleError checks for rate limit errors and sets backoff accordingly.
func (c *Client) handleError(tier int, err error) {
	var rlErr *slack.RateLimitedError
	if errors.As(err, &rlErr) {
		c.rateLimiter.HandleRateLimit(tier, rlErr.RetryAfter)
	}
}
