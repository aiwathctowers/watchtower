package jira

import (
	"encoding/json"
	"fmt"
	"log"

	"watchtower/internal/db"
)

// ComputeWhoToPing determines the best people to contact about a Jira issue.
// It returns up to maxPingTargets targets, deduplicated by SlackUserID, in priority order:
//  1. Root-cause assignee (last in blocking chain) — reason "assignee_blocker"
//  2. Issue's own assignee — reason "assignee"
//  3. Component expert (top resolver for the same component) — reason "expert"
//  4. Reporter — reason "reporter"
//  5. Slack participants from linked messages — reason "slack_participant"
func ComputeWhoToPing(d *db.DB, issue db.JiraIssue, chain []string) []PingTarget {
	seen := make(map[string]bool)
	var targets []PingTarget

	addTarget := func(slackID, name, reason string) {
		if slackID == "" || seen[slackID] || len(targets) >= maxPingTargets {
			return
		}
		seen[slackID] = true
		targets = append(targets, PingTarget{
			SlackUserID: slackID,
			DisplayName: name,
			Reason:      reason,
		})
	}

	// 1. Root cause assignee (last in chain, if chain has >1 elements).
	if len(chain) > 1 {
		rootKey := chain[len(chain)-1]
		rootIssue, err := d.GetJiraIssueByKey(rootKey)
		if err == nil && rootIssue != nil {
			addTarget(rootIssue.AssigneeSlackID, rootIssue.AssigneeDisplayName, "assignee_blocker")
		}
	}

	// 2. Issue's own assignee.
	addTarget(issue.AssigneeSlackID, issue.AssigneeDisplayName, "assignee")

	// 3. Component expert — person with most resolved issues in the same component.
	if len(targets) < maxPingTargets {
		components := parseComponents(issue.Components)
		for _, comp := range components {
			if len(targets) >= maxPingTargets {
				break
			}
			resolvers, err := d.GetTopResolversByComponent(comp, maxPingTargets)
			if err != nil {
				log.Printf("who_to_ping: failed to get resolvers for component %s: %v", comp, err)
				continue
			}
			for _, r := range resolvers {
				addTarget(r.SlackUserID, r.DisplayName, "expert")
				if len(targets) >= maxPingTargets {
					break
				}
			}
		}
	}

	// 4. Reporter.
	addTarget(issue.ReporterSlackID, issue.ReporterDisplayName, "reporter")

	// 5. Slack participants from jira_slack_links -> messages.
	if len(targets) < maxPingTargets {
		slackParticipants := fetchSlackParticipants(issue.Key, d)
		for _, p := range slackParticipants {
			addTarget(p.SlackUserID, p.DisplayName, "slack_participant")
		}
	}

	return targets
}

// ComputeWhoToPingForEpic determines who to contact about an epic.
// It returns up to maxPingTargets targets:
//  1. Epic owner (assignee of the epic issue itself) — reason "assignee"
//  2. Component experts across all child issues — reason "expert"
//  3. Child issue assignees (most active first) — reason "assignee"
func ComputeWhoToPingForEpic(d *db.DB, epicKey string) ([]PingTarget, error) {
	epicIssue, err := d.GetJiraIssueByKey(epicKey)
	if err != nil {
		return nil, fmt.Errorf("loading epic %s: %w", epicKey, err)
	}
	if epicIssue == nil {
		return nil, nil
	}

	children, err := d.GetJiraIssuesByEpicKey(epicKey)
	if err != nil {
		return nil, fmt.Errorf("loading children for epic %s: %w", epicKey, err)
	}

	seen := make(map[string]bool)
	var targets []PingTarget

	addTarget := func(slackID, name, reason string) {
		if slackID == "" || seen[slackID] || len(targets) >= maxPingTargets {
			return
		}
		seen[slackID] = true
		targets = append(targets, PingTarget{
			SlackUserID: slackID,
			DisplayName: name,
			Reason:      reason,
		})
	}

	// 1. Epic owner.
	addTarget(epicIssue.AssigneeSlackID, epicIssue.AssigneeDisplayName, "assignee")

	// 2. Component experts from epic's components.
	epicComponents := parseComponents(epicIssue.Components)
	// Also collect components from children.
	compSet := make(map[string]bool)
	for _, c := range epicComponents {
		compSet[c] = true
	}
	for _, child := range children {
		for _, c := range parseComponents(child.Components) {
			compSet[c] = true
		}
	}
	for comp := range compSet {
		if len(targets) >= maxPingTargets {
			break
		}
		resolvers, err := d.GetTopResolversByComponent(comp, maxPingTargets)
		if err != nil {
			continue
		}
		for _, r := range resolvers {
			addTarget(r.SlackUserID, r.DisplayName, "expert")
			if len(targets) >= maxPingTargets {
				break
			}
		}
	}

	// 3. Child issue assignees.
	for _, child := range children {
		addTarget(child.AssigneeSlackID, child.AssigneeDisplayName, "assignee")
		if len(targets) >= maxPingTargets {
			break
		}
	}

	return targets, nil
}

// parseComponents extracts component names from the JSON array stored in the components column.
func parseComponents(componentsJSON string) []string {
	if componentsJSON == "" || componentsJSON == "[]" {
		return nil
	}
	var components []string
	if err := json.Unmarshal([]byte(componentsJSON), &components); err != nil {
		return nil
	}
	return components
}
