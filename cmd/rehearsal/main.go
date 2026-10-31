// Command rehearsal loads only the approved synthetic fixture into an empty,
// explicitly named loopback database. It never sends messages or contacts an IdP.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/tjrasche/HerbsfestOS/internal/database"
	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	dsn := os.Getenv("DATABASE_URL")
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || !strings.HasSuffix(u.Path, "_rehearsal") {
		return fmt.Errorf("requires loopback PostgreSQL database ending _rehearsal")
	}
	db, p, e := database.Open(context.Background(), dsn)
	if e != nil {
		return e
	}
	defer p.Close()
	if e = s.Migrate(context.Background(), db, s.LegacyEdition{}); e != nil {
		return e
	}
	if e = s.Bootstrap(context.Background(), db, "urn:herbstfest:local", "developer"); e != nil {
		return e
	}
	var f struct {
		Edition struct {
			Year, StartDay, EndDay string `json:"-"`
		}
		Stands  []struct{ ID, Name string }
		Workers []struct {
			ID     string
			First  string   `json:"first_name"`
			Last   string   `json:"last_name"`
			Stands []string `json:"member_stands"`
		}
		Shifts []struct {
			ID, Stand string
			Start     string `json:"start_local"`
			End       string `json:"end_local"`
			Capacity  int
			Assigned  []string `json:"assigned_workers"`
			Pending   []string `json:"pending_workers"`
		}
	}
	data, e := os.ReadFile("docs/wayfinder/volunteer-scheduling/validation/rehearsal-fixture.json")
	if e != nil {
		return e
	}
	if e = json.Unmarshal(data, &f); e != nil {
		return e
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT revision FROM scheduling_state WHERE id=1 FOR UPDATE").Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Raw("SELECT count(*) FROM editions").Scan(&n).Error; e != nil {
			return e
		}
		if n != 0 {
			return fmt.Errorf("rehearsal database must have no editions")
		}
		if e := tx.Exec("INSERT INTO editions(id,label,start_day,end_day) VALUES('rehearsal','2027-rehearsal','2027-09-10','2027-09-12')").Error; e != nil {
			return e
		}
		var account string
		tx.Raw("SELECT id FROM accounts WHERE issuer='urn:herbstfest:local' AND subject='developer'").Scan(&account)
		for _, stand := range f.Stands {
			if e := tx.Exec("INSERT INTO stands(id,name) VALUES(?,?)", stand.ID, stand.Name).Error; e != nil {
				return e
			}
			if e := tx.Exec("INSERT INTO participations(id,edition_id,stand_id) VALUES(?,'rehearsal',?)", stand.ID, stand.ID).Error; e != nil {
				return e
			}
			if e := tx.Exec("INSERT INTO teams(participation_id,account_id) VALUES(?,?)", stand.ID, account).Error; e != nil {
				return e
			}
		}
		for _, w := range f.Workers {
			if e := tx.Exec("INSERT INTO workers(id,first_name,last_name) VALUES(?,?,?)", w.ID, w.First, w.Last).Error; e != nil {
				return e
			}
			for _, stand := range w.Stands {
				if e := tx.Exec("INSERT INTO memberships(stand_id,worker_id) VALUES(?,?)", stand, w.ID).Error; e != nil {
					return e
				}
			}
		}
		for _, sh := range f.Shifts {
			start, e := time.ParseInLocation("2006-01-02T15:04:05", sh.Start, s.Berlin)
			if e != nil {
				return e
			}
			end, e := time.ParseInLocation("2006-01-02T15:04:05", sh.End, s.Berlin)
			if e != nil {
				return e
			}
			if e := tx.Exec("INSERT INTO shifts(id,participation_id,title,starts_at,ends_at,capacity) VALUES(?,?,?,?,?,?)", sh.ID, sh.Stand, "Festdienst", start, end, sh.Capacity).Error; e != nil {
				return e
			}
			for _, worker := range sh.Assigned {
				if e := tx.Exec("INSERT INTO assignments(shift_id,worker_id) VALUES(?,?)", sh.ID, worker).Error; e != nil {
					return e
				}
			}
			for _, worker := range sh.Pending {
				if e := tx.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version) VALUES(?,?,?,'pending',1)", s.ID(), sh.ID, worker).Error; e != nil {
					return e
				}
			}
		}
		token := "synthetic-rehearsal-registration-only"
		h := sha256.Sum256([]byte(token))
		if e := tx.Exec("INSERT INTO capabilities(digest,kind,participation_id) VALUES(?,'signup','stand-00')", hex.EncodeToString(h[:])).Error; e != nil {
			return e
		}
		if e := tx.Exec("UPDATE scheduling_state SET current_edition='rehearsal',revision=revision+1 WHERE id=1").Error; e != nil {
			return e
		}
		fmt.Printf("Synthetic rehearsal loaded: %d stands, %d workers, %d shifts. Signup: /register/%s\n", len(f.Stands), len(f.Workers), len(f.Shifts), token)
		return nil
	})
}
