package jira

import (
	"fmt"
	"sort"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// EpicProgressEntry holds computed progress and forecast for a single epic.
type EpicProgressEntry struct {
	EpicKey          string
	EpicName         string // summary of the epic issue from jira_issues
	TotalIssues      int
	DoneIssues       int
	InProgressIssues int
	ProgressPct      float64 // done/total * 100
	WeeklyDeltaPct   float64 // resolved this week / total * 100
	StatusBadge      string  // "on_track", "at_risk", "behind"
	StatusReason     string  // human-readable reason for the badge
	ForecastWeeks    float64 // (total - done) / velocity_per_week
	ForecastDate     string  // ISO date when epic is expected to finish (empty if no velocity)
	VelocityPerWeek  float64 // resolved last 28 days / 4
	DueDate          string  // epic's due date (empty if none)
	DaysLate         int     // >0 means forecast is N days past due; <=0 means on time or no due date
}

// minEpicIssues is the minimum number of child issues for an epic to be included.
const minEpicIssues = 3

// maxForecastWeeks is used when velocity is zero (effectively infinite).
const maxForecastWeeks = 999.0

// ComputeEpicProgress calculates progress, velocity, forecast and status for
// all epics that have at least minEpicIssues child issues.
// Returns nil, nil when the epic_progress feature is disabled.
func ComputeEpicProgress(database *db.DB, cfg *config.Config, now time.Time) ([]EpicProgressEntry, error) {
	if !IsFeatureEnabled(cfg, "epic_progress") {
		return nil, nil
	}

	weekAgo := now.AddDate(0, 0, -7).Format(time.RFC3339)
	fourWeeksAgo := now.AddDate(0, 0, -28).Format(time.RFC3339)

	aggs, err := database.GetJiraEpicAggregates(weekAgo, fourWeeksAgo)
	if err != nil {
		return nil, err
	}

	if len(aggs) == 0 {
		return nil, nil
	}

	// Collect epic keys for bulk name lookup.
	epicKeys := make([]string, 0, len(aggs))
	for _, a := range aggs {
		epicKeys = append(epicKeys, a.EpicKey)
	}

	epicIssues, err := database.GetJiraIssuesByKeysMap(epicKeys)
	if err != nil {
		return nil, err
	}

	var entries []EpicProgressEntry
	for _, a := range aggs {
		if a.Total < minEpicIssues {
			continue
		}

		progressPct := float64(a.Done) / float64(a.Total) * 100
		weeklyDelta := float64(a.ResolvedLastWeek) / float64(a.Total) * 100
		velocity := float64(a.ResolvedLast4W) / 4.0

		var forecast float64
		remaining := float64(a.Total - a.Done)
		if velocity > 0 {
			forecast = remaining / velocity
		} else {
			forecast = maxForecastWeeks
		}

		epicName := ""
		dueDate := ""
		if issue, ok := epicIssues[a.EpicKey]; ok {
			epicName = issue.Summary
			dueDate = issue.DueDate
		}

		// Compute forecast date.
		var forecastDate string
		var daysLate int
		if velocity > 0 && forecast < maxForecastWeeks {
			fd := now.AddDate(0, 0, int(forecast*7))
			forecastDate = fd.Format("2006-01-02")

			// Compare with due date.
			if dueDate != "" {
				if due, err := time.Parse("2006-01-02", dueDate); err == nil {
					diff := fd.Sub(due)
					if diff > 0 {
						daysLate = int(diff.Hours() / 24)
					}
				}
			}
		} else if dueDate != "" {
			// No velocity but has due date — check if already overdue.
			if due, err := time.Parse("2006-01-02", dueDate); err == nil {
				if now.After(due) {
					daysLate = int(now.Sub(due).Hours() / 24)
				}
			}
		}

		badge, reason := computeStatusBadge(a, velocity, dueDate, forecastDate, daysLate, now)

		entries = append(entries, EpicProgressEntry{
			EpicKey:          a.EpicKey,
			EpicName:         epicName,
			TotalIssues:      a.Total,
			DoneIssues:       a.Done,
			InProgressIssues: a.InProgress,
			ProgressPct:      progressPct,
			WeeklyDeltaPct:   weeklyDelta,
			StatusBadge:      badge,
			StatusReason:     reason,
			ForecastWeeks:    forecast,
			ForecastDate:     forecastDate,
			VelocityPerWeek:  velocity,
			DueDate:          dueDate,
			DaysLate:         daysLate,
		})
	}

	// Sort: at_risk/behind first, then by progress_pct ASC.
	sort.Slice(entries, func(i, j int) bool {
		oi := badgeOrder(entries[i].StatusBadge)
		oj := badgeOrder(entries[j].StatusBadge)
		if oi != oj {
			return oi < oj
		}
		return entries[i].ProgressPct < entries[j].ProgressPct
	})

	return entries, nil
}

// computeStatusBadge determines the status badge and human-readable reason
// based on velocity, weekly progress, and deadline forecast.
func computeStatusBadge(a db.EpicAggRow, velocity float64, dueDate, forecastDate string, daysLate int, now time.Time) (string, string) {
	remaining := a.Total - a.Done
	if remaining == 0 {
		return "on_track", "All issues done"
	}

	// Deadline-aware checks first.
	if daysLate > 0 && velocity > 0 {
		weeks := float64(daysLate) / 7
		return "behind", fmt.Sprintf("Forecast %s, due %s — ~%.0f wk late", forecastDate, dueDate, weeks)
	}
	if daysLate > 0 && velocity == 0 {
		return "behind", fmt.Sprintf("No velocity, already %dd past due date %s", daysLate, dueDate)
	}

	if velocity == 0 {
		return "behind", "No issues resolved in the last 4 weeks"
	}

	if a.ResolvedLastWeek == 0 {
		reason := "No issues resolved this week"
		if dueDate != "" {
			if due, err := time.Parse("2006-01-02", dueDate); err == nil {
				daysLeft := int(due.Sub(now).Hours() / 24)
				if daysLeft > 0 {
					reason += fmt.Sprintf(", %dd until due date", daysLeft)
				}
			}
		}
		return "behind", reason
	}

	// Check if forecast misses deadline even with current velocity.
	if dueDate != "" && forecastDate != "" && forecastDate > dueDate {
		weeks := float64(daysLate) / 7
		if weeks < 1 {
			return "at_risk", fmt.Sprintf("Tight — forecast %s, due %s", forecastDate, dueDate)
		}
		return "at_risk", fmt.Sprintf("Forecast %s, due %s — ~%.0f wk late", forecastDate, dueDate, weeks)
	}

	if float64(a.ResolvedLastWeek) < velocity {
		pct := int((1.0 - float64(a.ResolvedLastWeek)/velocity) * 100)
		return "at_risk", fmt.Sprintf("Velocity dropped %d%% vs avg (%d vs %.1f/wk)", pct, a.ResolvedLastWeek, velocity)
	}

	// On track — include deadline info if available.
	if dueDate != "" && forecastDate != "" {
		return "on_track", fmt.Sprintf("On pace — forecast %s, due %s", forecastDate, dueDate)
	}
	return "on_track", fmt.Sprintf("%.1f issues/wk, %d remaining", velocity, remaining)
}

// badgeOrder returns sort priority (lower = first).
func badgeOrder(badge string) int {
	switch badge {
	case "behind":
		return 0
	case "at_risk":
		return 1
	case "on_track":
		return 2
	default:
		return 3
	}
}
