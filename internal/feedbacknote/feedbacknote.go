package feedbacknote

import "time"

type FeedbackNote struct {
	ID        uint64 `gorm:"primaryKey"`
	Title     string `gorm:"size:200;not null"`
	CreatedAt time.Time
}
