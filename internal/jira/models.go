// Package jira provides Jira Cloud integration for Watchtower.
package jira

// Board represents a Jira agile board.
type Board struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Location struct {
		ProjectKey  string `json:"projectKey"`
		ProjectName string `json:"projectName"`
	} `json:"location"`
}

// BoardList is a paginated response from the Jira agile boards API.
type BoardList struct {
	MaxResults int     `json:"maxResults"`
	StartAt    int     `json:"startAt"`
	Total      int     `json:"total"`
	IsLast     bool    `json:"isLast"`
	Values     []Board `json:"values"`
}

// Issue represents a Jira issue.
type Issue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

// IssueFields holds the fields of a Jira issue.
type IssueFields struct {
	Summary     string       `json:"summary"`
	Description interface{}  `json:"description"`
	IssueType   IssueType    `json:"issuetype"`
	Status      Status       `json:"status"`
	Assignee    *User        `json:"assignee"`
	Reporter    *User        `json:"reporter"`
	Priority    *Priority    `json:"priority"`
	Created     string       `json:"created"`
	Updated     string       `json:"updated"`
	DueDate     *string      `json:"duedate"`
	Labels      []string     `json:"labels"`
	Components  []Component  `json:"components"`
	IssueLinks  []IssueLink  `json:"issuelinks"`
	Sprint      *Sprint      `json:"sprint"`
	Epic        *EpicRef     `json:"epic"`
	Parent      *ParentRef   `json:"parent"`
	Resolved    *string      `json:"resolutiondate"`
	FixVersions []FixVersion `json:"fixVersions"`
}

// IssueType represents the type of a Jira issue.
type IssueType struct {
	Name           string `json:"name"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel int    `json:"hierarchyLevel"`
}

// Status represents a Jira issue status.
type Status struct {
	Name           string         `json:"name"`
	StatusCategory StatusCategory `json:"statusCategory"`
}

// StatusCategory represents a Jira status category.
type StatusCategory struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// User represents a Jira user.
type User struct {
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
	Active       bool   `json:"active"`
}

// Priority represents a Jira issue priority.
type Priority struct {
	Name string `json:"name"`
}

// Component represents a Jira project component.
type Component struct {
	Name string `json:"name"`
}

// IssueLink represents a link between two Jira issues.
type IssueLink struct {
	ID           string        `json:"id"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *IssueRef     `json:"inwardIssue"`
	OutwardIssue *IssueRef     `json:"outwardIssue"`
}

// IssueLinkType describes the type of relationship between linked issues.
type IssueLinkType struct {
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// IssueRef is a lightweight reference to a Jira issue (key only).
type IssueRef struct {
	Key string `json:"key"`
}

// FixVersion represents a Jira fix version (release).
type FixVersion struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"releaseDate"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
}

// Sprint represents a Jira sprint.
type Sprint struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	Goal         string `json:"goal"`
	StartDate    string `json:"startDate"`
	EndDate      string `json:"endDate"`
	CompleteDate string `json:"completeDate"`
}

// EpicRef is a lightweight reference to a Jira epic.
type EpicRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ParentRef is a lightweight reference to a parent issue.
type ParentRef struct {
	Key string `json:"key"`
}

// SearchResult is a paginated response from the Jira search API.
type SearchResult struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// SprintList is a paginated response from the Jira sprints API.
type SprintList struct {
	MaxResults int      `json:"maxResults"`
	StartAt    int      `json:"startAt"`
	IsLast     bool     `json:"isLast"`
	Values     []Sprint `json:"values"`
}
