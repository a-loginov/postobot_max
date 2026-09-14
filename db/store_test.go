package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	d, err := New("sqlite", filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d
}

func TestRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	d := testDB(t)

	student, err := d.FindOrCreateStudent(ctx, 777)
	if err != nil {
		t.Fatalf("FindOrCreateStudent: %v", err)
	}
	if err := d.UpdateStudentProfile(ctx, student.ID, "Иван", "Иванов", "9А"); err != nil {
		t.Fatalf("UpdateStudentProfile: %v", err)
	}

	req := &Request{StudentID: student.ID, Description: "заменить бутылку воды", Normalized: "заменить бутылку воды", Status: StatusActive}
	if err := d.CreateRequest(ctx, req); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	list, err := d.ResponsibleList(ctx)
	if err != nil {
		t.Fatalf("ResponsibleList: %v", err)
	}
	if len(list) != 1 || list[0].Student.Name != "Иван" {
		t.Fatalf("ResponsibleList got %+v", list)
	}

	dup, err := d.FindRecentDuplicate(ctx, student.ID, "заменить бутылку воды", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("FindRecentDuplicate: %v", err)
	}
	if dup == nil {
		t.Fatal("expected duplicate")
	}

	if err := d.UpdateStatus(ctx, req.ID, StatusDone, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	updated, err := d.GetRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if updated.Status != StatusDone || updated.DoneAt == nil {
		t.Fatalf("status not done: %+v", updated)
	}

	// Archive month export queries.
	monthReqs, err := d.MonthRequests(ctx, time.Now().Year(), time.Now().Month())
	if err != nil {
		t.Fatalf("MonthRequests: %v", err)
	}
	if len(monthReqs) != 1 {
		t.Fatalf("MonthRequests got %d", len(monthReqs))
	}

	if err := d.DeleteByID(ctx, req.ID); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
}

func TestPriorityReorder(t *testing.T) {
	ctx := context.Background()
	d := testDB(t)

	student, _ := d.FindOrCreateStudent(ctx, 888)
	r1 := &Request{StudentID: student.ID, Description: "стул", Normalized: "стул", Status: StatusActive}
	r2 := &Request{StudentID: student.ID, Description: "вода", Normalized: "вода", Status: StatusActive}
	d.CreateRequest(ctx, r1)
	d.CreateRequest(ctx, r2)

	if err := d.BumpPriority(ctx, r1.ID, 1); err != nil {
		t.Fatalf("BumpPriority: %v", err)
	}

	list, _ := d.ResponsibleList(ctx)
	// After bumping r1, it should come before r2 (order by priority desc).
	if list[0].ID != r1.ID {
		t.Fatalf("expected r1 first, got %d", list[0].ID)
	}
}
