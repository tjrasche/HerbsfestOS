package feedbacknote

import (
	"context"

	"gorm.io/gorm"
)

type Repository interface {
	List(context.Context) ([]FeedbackNote, error)
	Create(context.Context, *FeedbackNote) error
}

type PostgresRepository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context) ([]FeedbackNote, error) {
	var notes []FeedbackNote
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Find(&notes).Error
	return notes, err
}

func (r *PostgresRepository) Create(ctx context.Context, note *FeedbackNote) error {
	return r.db.WithContext(ctx).Create(note).Error
}
