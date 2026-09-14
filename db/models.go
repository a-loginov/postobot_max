package db

import "time"

type Student struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	MaxUserID int64     `gorm:"uniqueIndex" json:"max_user_id"`
	Name      string    `json:"name"`
	Surname   string    `json:"surname"`
	Class     string    `json:"class"`
	CreatedAt time.Time `json:"created_at"`

	Requests []Request `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

type RequestStatus string

const (
	StatusPendingModeration RequestStatus = "pending_moderation"
	StatusRejected          RequestStatus = "rejected"
	StatusActive            RequestStatus = "active"
	StatusDone              RequestStatus = "done"
)

type Request struct {
	ID            uint          `gorm:"primarykey" json:"id"`
	StudentID     uint          `gorm:"index" json:"student_id"`
	Student       Student       `json:"student"`
	Description   string        `json:"description"`
	Normalized    string        `gorm:"index" json:"-"`
	PhotoToken    string        `json:"photo_token"`
	PhotoURL      string        `json:"photo_url"`
	Status        RequestStatus `gorm:"index;default:pending_moderation" json:"status"`
	RejectReason  string        `json:"reject_reason,omitempty"`
	Priority      int           `gorm:"default:0" json:"priority"`
	DuplicateOfID *uint         `json:"duplicate_of_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	ModeratedAt   *time.Time    `json:"moderated_at,omitempty"`
	DoneAt        *time.Time    `json:"done_at,omitempty"`
}
