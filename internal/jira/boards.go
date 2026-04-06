package jira

import (
	"context"
	"fmt"
	"net/url"
)

// FetchAllBoards fetches all agile boards with pagination.
func (c *Client) FetchAllBoards(ctx context.Context) ([]Board, error) {
	var all []Board
	startAt := 0

	for {
		params := url.Values{
			"startAt":    {fmt.Sprintf("%d", startAt)},
			"maxResults": {"50"},
		}
		var resp BoardList
		if err := c.getWithQuery(ctx, "/rest/agile/1.0/board", params, &resp); err != nil {
			return nil, fmt.Errorf("fetching boards (startAt=%d): %w", startAt, err)
		}

		all = append(all, resp.Values...)

		if resp.IsLast || len(resp.Values) == 0 {
			break
		}
		startAt += len(resp.Values)
	}

	return all, nil
}

// FetchBoardIssueCount returns the total number of issues for a given board using its filter JQL.
func (c *Client) FetchBoardIssueCount(ctx context.Context, boardID int) (int, error) {
	// Use the board's backlog endpoint with maxResults=0 to just get the total.
	params := url.Values{
		"startAt":    {"0"},
		"maxResults": {"0"},
	}
	var result SearchResult
	path := fmt.Sprintf("/rest/agile/1.0/board/%d/issue", boardID)
	if err := c.getWithQuery(ctx, path, params, &result); err != nil {
		return 0, fmt.Errorf("fetching issue count for board %d: %w", boardID, err)
	}
	return result.Total, nil
}
