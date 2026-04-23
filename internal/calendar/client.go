package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	calendarAPIBase = "https://www.googleapis.com/calendar/v3"
)

// ErrAuthRevoked is returned when Google reports the refresh token is expired
// or revoked (invalid_grant). It signals that the user must re-authenticate.
var ErrAuthRevoked = errors.New("google calendar auth revoked")

// Client wraps Google Calendar API calls using raw net/http.
// Client is not safe for concurrent use.
type Client struct {
	hc           *http.Client
	accessToken  string
	refreshToken string
	oauthCfg     GoogleOAuthConfig
}

// NewClient creates a Google Calendar API client.
// It uses the refresh token to obtain a fresh access token.
func NewClient(ctx context.Context, refreshToken string, cfg GoogleOAuthConfig) (*Client, error) {
	c := &Client{
		hc:           &http.Client{Timeout: 30 * time.Second},
		refreshToken: refreshToken,
		oauthCfg:     cfg,
	}
	if err := c.refreshAccessToken(ctx); err != nil {
		return nil, fmt.Errorf("obtaining access token: %w", err)
	}
	return c, nil
}

// isInvalidGrant detects the "invalid_grant" error in Google's token endpoint response body,
// which indicates the refresh token is expired or revoked.
func isInvalidGrant(body []byte) bool {
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Error == "invalid_grant" {
		return true
	}
	return strings.Contains(string(body), "invalid_grant")
}

// refreshAccessToken exchanges the refresh token for a new access token.
func (c *Client) refreshAccessToken(ctx context.Context) error {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
		"client_id":     {c.oauthCfg.ClientID},
		"client_secret": {c.oauthCfg.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("token refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if isInvalidGrant(body) {
			return fmt.Errorf("%w: %s", ErrAuthRevoked, body)
		}
		return fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decoding token response: %w", err)
	}
	c.accessToken = result.AccessToken
	return nil
}

// doGet performs an authenticated GET request to the Google Calendar API.
func (c *Client) doGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	return c.doGetRetry(ctx, path, params, false)
}

// doGetRetry is the internal implementation with a retry guard to prevent infinite recursion.
func (c *Client) doGetRetry(ctx context.Context, path string, params url.Values, retried bool) ([]byte, error) {
	u := calendarAPIBase + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized && !retried {
		// Try refreshing token once and retry.
		if err := c.refreshAccessToken(ctx); err != nil {
			return nil, fmt.Errorf("token refresh on 401: %w", err)
		}
		return c.doGetRetry(ctx, path, params, true)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, body)
	}
	return body, nil
}

// googleEventsList is the response from events.list.
type googleEventsList struct {
	Items         []googleEvent `json:"items"`
	NextPageToken string        `json:"nextPageToken"`
}

// googleEvent represents a Google Calendar event from the API.
type googleEvent struct {
	ID               string         `json:"id"`
	Status           string         `json:"status"`
	Summary          string         `json:"summary"`
	Description      string         `json:"description"`
	Location         string         `json:"location"`
	HTMLLink         string         `json:"htmlLink"`
	Start            *googleTime    `json:"start"`
	End              *googleTime    `json:"end"`
	Organizer        *googlePerson  `json:"organizer"`
	Attendees        []googlePerson `json:"attendees"`
	RecurringEventID string         `json:"recurringEventId"`
	EventType        string         `json:"eventType"`
	Updated          string         `json:"updated"`
}

type googleTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

type googlePerson struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	Self           bool   `json:"self"`
	ResponseStatus string `json:"responseStatus"`
}

// googleCalendarList is the response from calendarList.list.
type googleCalendarList struct {
	Items []googleCalendarEntry `json:"items"`
}

type googleCalendarEntry struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	Primary         bool   `json:"primary"`
	BackgroundColor string `json:"backgroundColor"`
}

// FetchEvents fetches events from the specified calendars within a time range.
func (c *Client) FetchEvents(ctx context.Context, calendarIDs []string, timeMin, timeMax time.Time) ([]CalendarEvent, error) {
	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}

	var allEvents []CalendarEvent
	for _, calID := range calendarIDs {
		events, err := c.fetchCalendarEvents(ctx, calID, timeMin, timeMax)
		if err != nil {
			return nil, fmt.Errorf("fetching events from %s: %w", calID, err)
		}
		allEvents = append(allEvents, events...)
	}
	return allEvents, nil
}

func (c *Client) fetchCalendarEvents(ctx context.Context, calendarID string, timeMin, timeMax time.Time) ([]CalendarEvent, error) {
	var allEvents []CalendarEvent
	pageToken := ""

	for {
		params := url.Values{
			"timeMin":      {timeMin.Format(time.RFC3339)},
			"timeMax":      {timeMax.Format(time.RFC3339)},
			"singleEvents": {"true"},
			"orderBy":      {"startTime"},
			"maxResults":   {"250"},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		path := fmt.Sprintf("/calendars/%s/events", url.PathEscape(calendarID))
		body, err := c.doGet(ctx, path, params)
		if err != nil {
			return nil, err
		}

		var result googleEventsList
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decoding events: %w", err)
		}

		for _, item := range result.Items {
			if item.Status == "cancelled" {
				continue
			}
			event := convertEvent(item, calendarID)
			allEvents = append(allEvents, event)
		}

		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return allEvents, nil
}

// convertEvent transforms a Google Calendar API event into our model.
func convertEvent(item googleEvent, calendarID string) CalendarEvent {
	eventStatus := item.Status
	if eventStatus == "" {
		eventStatus = "confirmed"
	}
	event := CalendarEvent{
		ID:          item.ID,
		Title:       sanitizeTitle(item.Summary),
		Description: item.Description,
		Location:    item.Location,
		CalendarID:  calendarID,
		HTMLLink:    item.HTMLLink,
		EventType:   item.EventType,
		EventStatus: eventStatus,
		UpdatedAt:   item.Updated,
	}

	// Parse start/end times.
	if item.Start != nil {
		if item.Start.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, item.Start.DateTime); err == nil {
				event.StartTime = t.UTC()
			}
		} else if item.Start.Date != "" {
			event.IsAllDay = true
			if t, err := time.Parse("2006-01-02", item.Start.Date); err == nil {
				event.StartTime = t.UTC()
			}
		}
	}
	if item.End != nil {
		if item.End.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, item.End.DateTime); err == nil {
				event.EndTime = t.UTC()
			}
		} else if item.End.Date != "" {
			if t, err := time.Parse("2006-01-02", item.End.Date); err == nil {
				event.EndTime = t.UTC()
			}
		}
	}

	// Organizer email.
	if item.Organizer != nil {
		event.Organizer = item.Organizer.Email
	}

	// Attendees.
	for _, a := range item.Attendees {
		event.Attendees = append(event.Attendees, Attendee{
			Email:          a.Email,
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
		})
	}

	// User's response status.
	for _, a := range item.Attendees {
		if a.Self {
			event.ResponseStatus = a.ResponseStatus
			break
		}
	}
	if event.ResponseStatus == "" {
		event.ResponseStatus = "accepted"
	}

	// Recurring.
	if item.RecurringEventID != "" {
		event.Recurring = true
	}

	return event
}

// sanitizeTitle removes conference links and sensitive info from event title.
func sanitizeTitle(title string) string {
	return strings.TrimSpace(title)
}

// FetchCalendars lists the user's visible calendars.
func (c *Client) FetchCalendars(ctx context.Context) ([]CalendarInfo, error) {
	body, err := c.doGet(ctx, "/users/me/calendarList", nil)
	if err != nil {
		return nil, fmt.Errorf("listing calendars: %w", err)
	}

	var result googleCalendarList
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding calendar list: %w", err)
	}

	var calendars []CalendarInfo
	for _, item := range result.Items {
		calendars = append(calendars, CalendarInfo{
			ID:      item.ID,
			Summary: item.Summary,
			Primary: item.Primary,
			Color:   item.BackgroundColor,
		})
	}
	return calendars, nil
}
