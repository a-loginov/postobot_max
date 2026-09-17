package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("not found")

func (d *DB) FindOrCreateStudent(ctx context.Context, maxUserID int64) (*Student, error) {
	var s Student
	err := d.WithContext(ctx).Where("max_user_id = ?", maxUserID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	s = Student{MaxUserID: maxUserID}
	if err := d.WithContext(ctx).Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) UpdateStudentProfile(ctx context.Context, studentID uint, name, surname, class string) error {
	return d.WithContext(ctx).Model(&Student{}).
		Where("id = ?", studentID).
		Updates(map[string]any{"name": name, "surname": surname, "class": class}).Error
}

func (d *DB) UpdateStudentClass(ctx context.Context, studentID uint, class string) error {
	return d.WithContext(ctx).Model(&Student{}).
		Where("id = ?", studentID).
		Update("class", class).Error
}

func (d *DB) GetStudentByID(ctx context.Context, id uint) (*Student, error) {
	var s Student
	if err := d.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) CreateRequest(ctx context.Context, r *Request) error {
	return d.WithContext(ctx).Create(r).Error
}

func (d *DB) GetRequest(ctx context.Context, id uint) (*Request, error) {
	var r Request
	err := d.WithContext(ctx).Preload("Student").First(&r, id).Error
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// FindRecentDuplicate returns an existing active request of the same student
// whose normalized text is identical (same text, slightly reworded spaces/punct).
func (d *DB) FindRecentDuplicate(ctx context.Context, studentID uint, normalized string, since time.Time) (*Request, error) {
	var r Request
	err := d.WithContext(ctx).
		Where("student_id = ? AND normalized = ? AND status IN ? AND created_at >= ?",
			studentID, normalized, []RequestStatus{StatusActive, StatusPendingModeration}, since).
		First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ResponsibleList returns requests the responsible person works with:
// active requests first (by priority desc, then oldest), then pending ones.
func (d *DB) ResponsibleList(ctx context.Context) ([]Request, error) {
	var requests []Request
	if err := d.WithContext(ctx).
		Preload("Student").
		Where("status IN ?", []RequestStatus{StatusActive, StatusPendingModeration}).
		Order("priority DESC").
		Find(&requests).Error; err != nil {
		return nil, err
	}
	return requests, nil
}

func (d *DB) StudentRequests(ctx context.Context, studentID uint) ([]Request, error) {
	var requests []Request
	if err := d.WithContext(ctx).
		Where("student_id = ?", studentID).
		Order("created_at DESC").
		Find(&requests).Error; err != nil {
		return nil, err
	}
	return requests, nil
}

func (d *DB) UpdateStatus(ctx context.Context, id uint, status RequestStatus, reason string) error {
	updates := map[string]any{"status": status, "reject_reason": reason}
	switch status {
	case StatusActive:
		updates["moderated_at"] = time.Now()
	case StatusDone:
		updates["done_at"] = time.Now()
	}

	return d.WithContext(ctx).Model(&Request{}).Where("id = ?", id).Updates(updates).Error
}

// ReorderPriority applies a manual +delta priority change to a request
// and persists the new priority value.
func (d *DB) BumpPriority(ctx context.Context, id uint, delta int) error {
	return d.WithContext(ctx).Model(&Request{}).
		Where("id = ?", id).
		Update("priority", gorm.Expr("priority + ?", delta)).Error
}

// MonthRequests returns all requests of a given month (local time),
// used by the archive export script.
func (d *DB) MonthRequests(ctx context.Context, year int, month time.Month) ([]Request, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 1, 0)

	var requests []Request
	if err := d.WithContext(ctx).
		Preload("Student").
		Where("created_at >= ? AND created_at < ?", start, end).
		Order("created_at ASC").
		Find(&requests).Error; err != nil {
		return nil, err
	}
	return requests, nil
}

// DeleteByID removes a single request (used after archival).
func (d *DB) DeleteByID(ctx context.Context, id uint) error {
	return d.WithContext(ctx).Delete(&Request{}, id).Error
}
