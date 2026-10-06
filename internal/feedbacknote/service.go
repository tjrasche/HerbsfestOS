package feedbacknote

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidTitle = errors.New("title must contain between 1 and 200 characters")

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context) ([]FeedbackNote, error) {
	return s.repository.List(ctx)
}

func (s *Service) Create(ctx context.Context, title string) error {
	title = strings.TrimSpace(title)
	if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 200 {
		return ErrInvalidTitle
	}
	return s.repository.Create(ctx, &FeedbackNote{Title: title})
}
