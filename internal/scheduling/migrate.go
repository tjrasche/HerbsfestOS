package scheduling

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql authorization-model.json
var Files embed.FS

func ID() string { return rand.Text() }

// Migrate runs at deployment preparation. The initial scheduling migration
// discards disposable, unscoped development feedback. Versioned/scoped data is
// preserved on every rerun. All changes commit or roll back together.
func Migrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(71420931)").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())").Error; err != nil {
			return err
		}
		var done int64
		if err := tx.Raw("SELECT count(*) FROM schema_migrations WHERE version=1").Scan(&done).Error; err != nil {
			return err
		}
		if done > 0 {
			return migratePreparation(tx)
		}
		var exists bool
		if err := tx.Raw("SELECT to_regclass('feedback_notes') IS NOT NULL").Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			if err := tx.Exec("CREATE TABLE feedback_notes(id bigserial PRIMARY KEY,title varchar(200) NOT NULL,created_at timestamptz)").Error; err != nil {
				return err
			}
		}
		sql, err := Files.ReadFile("migrations/001.sql")
		if err != nil {
			return err
		}
		if err = tx.Exec(string(sql)).Error; err != nil {
			return err
		}
		if err = tx.Exec("ALTER TABLE feedback_notes ADD COLUMN edition_id text REFERENCES editions(id)").Error; err != nil {
			return err
		}
		// Explicitly authorized development reset. Delete only unscoped notes;
		// the migration version gate above prevents this on later deployments.
		if err = tx.Exec("DELETE FROM feedback_notes WHERE edition_id IS NULL").Error; err != nil {
			return err
		}

		if err = tx.Exec("ALTER TABLE feedback_notes ALTER COLUMN edition_id SET NOT NULL").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO schema_migrations(version) VALUES(1)").Error; err != nil {
			return err
		}
		return migratePreparation(tx)
	})
}

func validDates(label, start, end string) bool {
	a, e := time.Parse("2006-01-02", start)
	b, f := time.Parse("2006-01-02", end)
	return strings.TrimSpace(label) != "" && len(label) <= 80 && e == nil && f == nil && !b.Before(a)
}

// Bootstrap is deliberately separate from migration and never infers a role
// from an identity-provider claim. It is an operator-only maintenance command.
func Bootstrap(ctx context.Context, db *gorm.DB, issuer, subject string) error {
	if issuer == "" || subject == "" {
		return errors.New("explicit BOOTSTRAP_ISSUER and BOOTSTRAP_SUBJECT required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT revision FROM scheduling_state WHERE id=1 FOR UPDATE").Error; err != nil {
			return err
		}
		id := ID()
		if err := tx.Exec("INSERT INTO accounts(id,issuer,subject) VALUES(?,?,?) ON CONFLICT(issuer,subject) DO NOTHING", id, issuer, subject).Error; err != nil {
			return err
		}
		if err := tx.Raw("SELECT id FROM accounts WHERE issuer=? AND subject=?", issuer, subject).Scan(&id).Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO global_grants(account_id) VALUES(?) ON CONFLICT DO NOTHING", id).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE scheduling_state SET revision=revision+1 WHERE id=1").Error
	})
}

// BootstrapOnce records initial setup independently of live grants. Repeated
// reconciliation must never restore a grant later revoked through the app.
func BootstrapOnce(ctx context.Context, db *gorm.DB, issuer, subject string) error {
	if issuer == "" || subject == "" {
		return errors.New("explicit bootstrap issuer/subject required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT revision FROM scheduling_state WHERE id=1 FOR UPDATE").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Raw("SELECT count(*) FROM scheduling_bootstrap WHERE id=1").Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		var grants int64
		if err := tx.Raw("SELECT count(*) FROM global_grants").Scan(&grants).Error; err != nil {
			return err
		}
		if grants == 0 {
			if err := Bootstrap(ctx, tx, issuer, subject); err != nil {
				return err
			}
		}
		return tx.Exec("INSERT INTO scheduling_bootstrap(id,issuer,subject) VALUES(1,?,?)", issuer, subject).Error
	})
}

func migratePreparation(tx *gorm.DB) error {
	var count int64
	if err := tx.Raw("SELECT count(*) FROM schema_migrations WHERE version=2").Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	sql, err := Files.ReadFile("migrations/002.sql")
	if err != nil {
		return err
	}
	if err = tx.Exec(string(sql)).Error; err != nil {
		return err
	}
	return tx.Exec("INSERT INTO schema_migrations(version) VALUES(2)").Error
}
