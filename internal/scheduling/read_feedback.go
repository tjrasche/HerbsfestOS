package scheduling

import (
	"context"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type FeedbackPageModel struct {
	Shell   ui.ShellModel
	Rows    []Note
	Page    int
	HasNext bool
}

func (s *Store) FeedbackList(ctx context.Context, who Identity, edition string, page int) (FeedbackPageModel, error) {
	v := FeedbackPageModel{Page: max(page, 1)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.admin("view_feedback"); err != nil {
			return err
		}
		if shell.Selected == "" {
			return nil
		}
		if err := t.db.Raw("SELECT id,title,created_at FROM feedback_notes WHERE edition_id=? ORDER BY created_at DESC,id DESC LIMIT 26 OFFSET ?", shell.Selected, (v.Page-1)*25).Scan(&v.Rows).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Rows) > 25
		if v.HasNext {
			v.Rows = v.Rows[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
