package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const SettingStage = "stage"

// GetSetting returns a settings value or the provided fallback when unset.
func (d *DB) GetSetting(ctx context.Context, key, fallback string) (string, error) {
	var s Setting
	err := d.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	return s.Value, nil
}

// SetSetting upserts a settings value.
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	var s Setting
	err := d.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return d.WithContext(ctx).Create(&Setting{Key: key, Value: value}).Error
	}
	if err != nil {
		return err
	}
	s.Value = value
	return d.WithContext(ctx).Save(&s).Error
}

func (d *DB) IsBetaTester(ctx context.Context, maxUserID int64) (bool, error) {
	var count int64
	err := d.WithContext(ctx).Model(&BetaTester{}).
		Where("max_user_id = ?", maxUserID).Count(&count).Error
	return count > 0, err
}

func (d *DB) AddBetaTester(ctx context.Context, maxUserID int64) error {
	return d.WithContext(ctx).FirstOrCreate(&BetaTester{MaxUserID: maxUserID}, BetaTester{MaxUserID: maxUserID}).Error
}

func (d *DB) BetaTesters(ctx context.Context) ([]BetaTester, error) {
	var testers []BetaTester
	if err := d.WithContext(ctx).Order("added_at DESC").Find(&testers).Error; err != nil {
		return nil, err
	}
	return testers, nil
}

// --- Beta applications ---

func (d *DB) CreateBetaApplication(ctx context.Context, maxUserID int64, name, surname, class, reason string) error {
	return d.WithContext(ctx).Create(&BetaApplication{
		MaxUserID: maxUserID,
		Name:      name,
		Surname:   surname,
		Class:     class,
		Reason:    reason,
		Status:    BetaStatusPending,
	}).Error
}

func (d *DB) HasPendingBetaApplication(ctx context.Context, maxUserID int64) (bool, error) {
	var count int64
	err := d.WithContext(ctx).Model(&BetaApplication{}).
		Where("max_user_id = ? AND status = ?", maxUserID, BetaStatusPending).
		Count(&count).Error
	return count > 0, err
}

func (d *DB) PendingBetaApplications(ctx context.Context) ([]BetaApplication, error) {
	var apps []BetaApplication
	if err := d.WithContext(ctx).
		Where("status = ?", BetaStatusPending).
		Order("created_at ASC").Find(&apps).Error; err != nil {
		return nil, err
	}
	return apps, nil
}

func (d *DB) GetBetaApplication(ctx context.Context, id uint) (*BetaApplication, error) {
	var app BetaApplication
	if err := d.WithContext(ctx).First(&app, id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (d *DB) SetBetaApplicationStatus(ctx context.Context, id uint, status string) error {
	return d.WithContext(ctx).Model(&BetaApplication{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "reviewed_at": time.Now()}).Error
}

// Stats returns a quick overview for the admin panel.
type Stats struct {
	Students         int64
	Requests         int64
	ActiveRequests   int64
	DoneRequests     int64
	RejectedRequests int64
	BetaTesters      int64
}

func (d *DB) Stats(ctx context.Context) (*Stats, error) {
	var s Stats

	var students, requests, active, done, rejected, testers int64
	if err := d.WithContext(ctx).Model(&Student{}).Count(&students).Error; err != nil {
		return nil, err
	}
	if err := d.WithContext(ctx).Model(&Request{}).Count(&requests).Error; err != nil {
		return nil, err
	}
	if err := d.WithContext(ctx).Model(&Request{}).Where("status = ?", StatusActive).Count(&active).Error; err != nil {
		return nil, err
	}
	if err := d.WithContext(ctx).Model(&Request{}).Where("status = ?", StatusDone).Count(&done).Error; err != nil {
		return nil, err
	}
	if err := d.WithContext(ctx).Model(&Request{}).Where("status = ?", StatusRejected).Count(&rejected).Error; err != nil {
		return nil, err
	}
	if err := d.WithContext(ctx).Model(&BetaTester{}).Count(&testers).Error; err != nil {
		return nil, err
	}

	s.Students = students
	s.Requests = requests
	s.ActiveRequests = active
	s.DoneRequests = done
	s.RejectedRequests = rejected
	s.BetaTesters = testers
	return &s, nil
}
