package scheduling

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type MergePerson struct {
	Worker
	Memberships, Requests, Assignments int
}
type MergeComparison struct {
	Target, Source                        MergePerson
	Result                                Worker
	Fingerprint                           string
	EmailConflict, PhoneConflict, Blocked bool
}
type MergePage struct {
	Shell                     ui.ShellModel
	TargetID, SourceID, Query string
	Candidates                []DirectoryResult
	Preview                   *MergeComparison
	Page                      int
	HasNext                   bool
}

func mergeContactValid(a, b, result, choice string, clear bool) bool {
	if clear {
		return result == ""
	}
	if (a != "" || b != "") && result == "" {
		return false
	}
	if a != "" && b != "" && a != b {
		switch choice {
		case "target":
			return result == a
		case "source":
			return result == b
		case "custom":
			return result != ""
		default:
			return false
		}
	}
	return true
}
func (t *Tx) mergePreview(target, source string) (MergeComparison, error) {
	var v MergeComparison
	if target == "" || source == "" || target == source {
		return v, ErrInvalid
	}
	var workers []Worker
	if err := t.db.Raw("SELECT * FROM workers WHERE id IN (?,?) ORDER BY id", target, source).Scan(&workers).Error; err != nil {
		return v, err
	}
	if len(workers) != 2 {
		return v, ErrInvalid
	}
	for _, w := range workers {
		if w.ID == target {
			v.Target.Worker = w
		} else {
			v.Source.Worker = w
		}
	}
	var members []struct{ StandID, WorkerID string }
	var requests []Request
	var assignments []struct {
		ShiftID, WorkerID string
		Active            bool
	}
	var proofs []Capability
	if err := t.db.Raw("SELECT * FROM memberships WHERE worker_id IN (?,?) ORDER BY stand_id,worker_id", target, source).Scan(&members).Error; err != nil {
		return v, err
	}
	if err := t.db.Raw("SELECT * FROM requests WHERE worker_id IN (?,?) ORDER BY id", target, source).Scan(&requests).Error; err != nil {
		return v, err
	}
	if err := t.db.Raw("SELECT * FROM assignments WHERE worker_id IN (?,?) ORDER BY shift_id,worker_id", target, source).Scan(&assignments).Error; err != nil {
		return v, err
	}
	if err := t.db.Raw("SELECT * FROM capabilities WHERE worker_id IN (?,?) ORDER BY digest", target, source).Scan(&proofs).Error; err != nil {
		return v, err
	}
	for _, m := range members {
		if m.WorkerID == target {
			v.Target.Memberships++
		} else {
			v.Source.Memberships++
		}
	}
	for _, r := range requests {
		if r.WorkerID == target {
			v.Target.Requests++
		} else {
			v.Source.Requests++
		}
	}
	for _, a := range assignments {
		if a.Active {
			if a.WorkerID == target {
				v.Target.Assignments++
			} else {
				v.Source.Assignments++
			}
		}
	}
	encoded, err := json.Marshal([]any{workers, members, requests, assignments, proofs})
	if err != nil {
		return v, err
	}
	v.Fingerprint = digest(string(encoded))
	a, b := v.Target.Worker, v.Source.Worker
	v.Result = a
	v.Result.Email = nonempty(a.Email, b.Email)
	v.Result.Phone = nonempty(a.Phone, b.Phone)
	if a.Notes == "" {
		v.Result.Notes = b.Notes
	} else if b.Notes != "" && a.Notes != b.Notes {
		v.Result.Notes = a.Notes + "\n\n" + b.Notes
	}
	v.EmailConflict = a.Email != "" && b.Email != "" && a.Email != b.Email
	v.PhoneConflict = a.Phone != "" && b.Phone != "" && a.Phone != b.Phone
	v.Blocked = a.AccountID != nil && b.AccountID != nil && *a.AccountID != *b.AccountID
	return v, nil
}
func (s *Store) MergePreview(ctx context.Context, who Identity, target, source string) (MergeComparison, error) {
	var v MergeComparison
	err := s.transact(ctx, who, false, func(t *Tx) error {
		if err := t.admin("manage_catalog"); err != nil {
			return err
		}
		var err error
		v, err = t.mergePreview(target, source)
		return err
	})
	return v, err
}
func (s *Store) MergeCandidates(ctx context.Context, who Identity, edition, target, source, query string, page int) (MergePage, error) {
	v := MergePage{TargetID: target, SourceID: source, Query: strings.TrimSpace(query), Page: max(page, 1)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.admin("manage_catalog"); err != nil {
			return err
		}
		if target != "" && source != "" {
			p, err := t.mergePreview(target, source)
			if err != nil {
				return err
			}
			v.Preview = &p
		}
		if len(v.Query) >= 2 && len(v.Query) <= 200 {
			var rows []Worker
			if err := t.db.Raw("SELECT * FROM workers WHERE id<>? AND (position(lower(?) in lower(first_name||' '||last_name))>0 OR lower(email)=lower(?) OR phone=?) ORDER BY last_name,first_name,id LIMIT 26 OFFSET ?", target, v.Query, v.Query, v.Query, (v.Page-1)*25).Scan(&rows).Error; err != nil {
				return err
			}
			v.HasNext = len(rows) > 25
			if v.HasNext {
				rows = rows[:25]
			}
			for _, w := range rows {
				v.Candidates = append(v.Candidates, DirectoryResult{Worker: w, Full: true})
			}
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
