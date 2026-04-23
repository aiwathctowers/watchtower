package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dayPlanTestDB(t *testing.T) *DB {
	t.Helper()
	return openTestDB(t)
}

func TestCreateAndGetDayPlan(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	p := &DayPlan{
		UserID:          "U1",
		PlanDate:        "2026-04-23",
		Status:          DayPlanStatusActive,
		HasConflicts:    false,
		ConflictSummary: sql.NullString{},
		GeneratedAt:     now,
		RegenerateCount: 0,
		FeedbackHistory: "[]",
	}

	id, err := db.CreateDayPlan(p)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	got, err := db.GetDayPlan("U1", "2026-04-23")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, id, got.ID)
	assert.Equal(t, "U1", got.UserID)
	assert.Equal(t, "2026-04-23", got.PlanDate)
	assert.Equal(t, DayPlanStatusActive, got.Status)
	assert.False(t, got.HasConflicts)
	assert.Equal(t, "[]", got.FeedbackHistory)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestUpsertDayPlan_UpdatesExisting(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	p := &DayPlan{
		UserID:          "U1",
		PlanDate:        "2026-04-23",
		Status:          DayPlanStatusActive,
		GeneratedAt:     now,
		FeedbackHistory: "[]",
	}

	id1, err := db.UpsertDayPlan(p)
	require.NoError(t, err)
	assert.Greater(t, id1, int64(0))

	// Upsert again with same key but different status.
	p2 := &DayPlan{
		UserID:          "U1",
		PlanDate:        "2026-04-23",
		Status:          DayPlanStatusArchived,
		GeneratedAt:     now,
		FeedbackHistory: "[]",
	}
	id2, err := db.UpsertDayPlan(p2)
	require.NoError(t, err)

	// Same row — same id.
	assert.Equal(t, id1, id2)

	got, err := db.GetDayPlan("U1", "2026-04-23")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, DayPlanStatusArchived, got.Status)
}

func TestCreateDayPlanItems_AndGet(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	id, err := db.CreateDayPlan(&DayPlan{
		UserID:      "U1",
		PlanDate:    "2026-04-23",
		Status:      DayPlanStatusActive,
		GeneratedAt: now,
	})
	require.NoError(t, err)

	items := []DayPlanItem{
		{
			DayPlanID:  id,
			Kind:       DayPlanItemKindBacklog,
			SourceType: DayPlanItemSourceManual,
			Title:      "Manual backlog item",
			Status:     DayPlanItemStatusPending,
			OrderIndex: 1,
		},
		{
			DayPlanID:  id,
			Kind:       DayPlanItemKindTimeblock,
			SourceType: DayPlanItemSourceFocus,
			Title:      "Deep work block",
			Status:     DayPlanItemStatusPending,
			OrderIndex: 0,
			StartTime: sql.NullTime{
				Valid: true,
				Time:  now,
			},
		},
	}

	err = db.CreateDayPlanItems(id, items)
	require.NoError(t, err)

	got, err := db.GetDayPlanItems(id)
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Ordering: timeblock first (kind='timeblock' first), then backlog.
	assert.Equal(t, DayPlanItemKindTimeblock, got[0].Kind)
	assert.Equal(t, DayPlanItemKindBacklog, got[1].Kind)
}

func TestReplaceAIItems_PreservesManualAndCalendar(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	planID, err := db.CreateDayPlan(&DayPlan{
		UserID:      "U1",
		PlanDate:    "2026-04-23",
		Status:      DayPlanStatusActive,
		GeneratedAt: now,
	})
	require.NoError(t, err)

	seed := []DayPlanItem{
		{DayPlanID: planID, Kind: DayPlanItemKindBacklog, SourceType: DayPlanItemSourceManual, Title: "Manual item", Status: DayPlanItemStatusPending, OrderIndex: 0},
		{DayPlanID: planID, Kind: DayPlanItemKindTimeblock, SourceType: DayPlanItemSourceCalendar, Title: "Calendar meeting", Status: DayPlanItemStatusPending, OrderIndex: 1},
		{DayPlanID: planID, Kind: DayPlanItemKindTimeblock, SourceType: DayPlanItemSourceFocus, Title: "Old focus block", Status: DayPlanItemStatusPending, OrderIndex: 2},
		{DayPlanID: planID, Kind: DayPlanItemKindBacklog, SourceType: DayPlanItemSourceTask, Title: "Old task item", Status: DayPlanItemStatusPending, OrderIndex: 3},
	}
	err = db.CreateDayPlanItems(planID, seed)
	require.NoError(t, err)

	newItems := []DayPlanItem{
		{DayPlanID: planID, Kind: DayPlanItemKindTimeblock, SourceType: DayPlanItemSourceFocus, Title: "New focus block", Status: DayPlanItemStatusPending, OrderIndex: 0},
	}
	err = db.ReplaceAIItems(planID, newItems)
	require.NoError(t, err)

	got, err := db.GetDayPlanItems(planID)
	require.NoError(t, err)

	titles := make([]string, len(got))
	for i, item := range got {
		titles[i] = item.Title
	}

	assert.Contains(t, titles, "Manual item", "manual item should be preserved")
	assert.Contains(t, titles, "Calendar meeting", "calendar item should be preserved")
	assert.Contains(t, titles, "New focus block", "new AI item should be present")
	assert.NotContains(t, titles, "Old focus block", "old AI focus item should be gone")
	assert.NotContains(t, titles, "Old task item", "old AI task item should be gone")
	assert.Len(t, got, 3)
}

func TestUpdateItemStatus(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	planID, err := db.CreateDayPlan(&DayPlan{
		UserID:      "U1",
		PlanDate:    "2026-04-23",
		Status:      DayPlanStatusActive,
		GeneratedAt: now,
	})
	require.NoError(t, err)

	err = db.CreateDayPlanItems(planID, []DayPlanItem{
		{DayPlanID: planID, Kind: DayPlanItemKindBacklog, SourceType: DayPlanItemSourceManual, Title: "Item", Status: DayPlanItemStatusPending, OrderIndex: 0},
	})
	require.NoError(t, err)

	items, err := db.GetDayPlanItems(planID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	itemID := items[0].ID

	err = db.UpdateItemStatus(itemID, DayPlanItemStatusDone)
	require.NoError(t, err)

	updated, err := db.GetDayPlanItems(planID)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	assert.Equal(t, DayPlanItemStatusDone, updated[0].Status)
}

func TestDeleteDayPlanItem(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	planID, err := db.CreateDayPlan(&DayPlan{
		UserID:      "U1",
		PlanDate:    "2026-04-23",
		Status:      DayPlanStatusActive,
		GeneratedAt: now,
	})
	require.NoError(t, err)

	err = db.CreateDayPlanItems(planID, []DayPlanItem{
		{DayPlanID: planID, Kind: DayPlanItemKindBacklog, SourceType: DayPlanItemSourceManual, Title: "To delete", Status: DayPlanItemStatusPending, OrderIndex: 0},
		{DayPlanID: planID, Kind: DayPlanItemKindBacklog, SourceType: DayPlanItemSourceManual, Title: "To keep", Status: DayPlanItemStatusPending, OrderIndex: 1},
	})
	require.NoError(t, err)

	items, err := db.GetDayPlanItems(planID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	var deleteID int64
	for _, it := range items {
		if it.Title == "To delete" {
			deleteID = it.ID
		}
	}

	err = db.DeleteDayPlanItem(deleteID)
	require.NoError(t, err)

	remaining, err := db.GetDayPlanItems(planID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "To keep", remaining[0].Title)
}

func TestListDayPlans(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	dates := []string{"2026-04-21", "2026-04-23", "2026-04-22"}
	for _, d := range dates {
		_, err := db.CreateDayPlan(&DayPlan{
			UserID:      "U1",
			PlanDate:    d,
			Status:      DayPlanStatusActive,
			GeneratedAt: now,
		})
		require.NoError(t, err)
	}

	list, err := db.ListDayPlans("U1", 10)
	require.NoError(t, err)
	require.Len(t, list, 3)

	// DESC by plan_date: newest first.
	assert.Equal(t, "2026-04-23", list[0].PlanDate)
	assert.Equal(t, "2026-04-22", list[1].PlanDate)
	assert.Equal(t, "2026-04-21", list[2].PlanDate)
}

func TestSetHasConflicts(t *testing.T) {
	db := dayPlanTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	id, err := db.CreateDayPlan(&DayPlan{
		UserID:      "U1",
		PlanDate:    "2026-04-23",
		Status:      DayPlanStatusActive,
		GeneratedAt: now,
	})
	require.NoError(t, err)

	err = db.SetHasConflicts(id, true, "Meetings overlap at 10am")
	require.NoError(t, err)

	got, err := db.GetDayPlanByID(id)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.True(t, got.HasConflicts)
	assert.True(t, got.ConflictSummary.Valid)
	assert.Equal(t, "Meetings overlap at 10am", got.ConflictSummary.String)
}
