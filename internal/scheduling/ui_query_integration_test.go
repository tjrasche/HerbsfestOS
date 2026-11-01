package scheduling_test

import (
	"context"
	"strings"
	"testing"
	"time"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type queryRecorder struct{ statements []string }

func (l *queryRecorder) LogMode(logger.LogLevel) logger.Interface    { return l }
func (*queryRecorder) Info(context.Context, string, ...interface{})  {}
func (*queryRecorder) Warn(context.Context, string, ...interface{})  {}
func (*queryRecorder) Error(context.Context, string, ...interface{}) {}
func (l *queryRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	q, _ := fc()
	l.statements = append(l.statements, q)
}

type checkRecorder struct {
	checker s.Checker
	count   int
}

func (c *checkRecorder) CheckContext(ctx context.Context, user, relation, object string, facts []s.Tuple) (bool, error) {
	c.count++
	return c.checker.CheckContext(ctx, user, relation, object, facts)
}

func TestScheduleSummaryQueriesAreBoundedByStandPage(t *testing.T) {
	app, db := setup(t)
	edition, part, _ := baseline(t, app)
	var actor string
	must(t, db.Raw("SELECT id FROM accounts WHERE issuer=? AND subject=?", admin.Issuer, admin.Subject).Scan(&actor).Error)
	// This isolated fixture intentionally writes trusted synthetic rows directly;
	// the assertion concerns read scaling, while command semantics have their own tests.
	for i := 1; i < 10; i++ {
		id := "summary-stand-" + string(rune('a'+i))
		must(t, db.Exec("INSERT INTO stands(id,name) VALUES(?,?)", id, id).Error)
		must(t, db.Exec("INSERT INTO participations(id,stand_id,edition_id) SELECT ?,?,edition_id FROM participations WHERE id=?", id, id, part).Error)
		must(t, db.Exec("INSERT INTO teams(participation_id,account_id) VALUES(?,?)", id, actor).Error)
	}
	must(t, db.Exec("DELETE FROM shifts").Error)
	must(t, db.Exec("INSERT INTO shifts(id,participation_id,title,starts_at,ends_at,capacity) SELECT 'summary-shift-'||p.id||'-'||g,p.id,'Zeitfenster '||g,'2027-09-10 08:00:00+02'::timestamptz + g * interval '20 minutes','2027-09-10 08:30:00+02'::timestamptz + g * interval '20 minutes',4 FROM participations p CROSS JOIN generate_series(1,15) g").Error)
	queries := &queryRecorder{}
	checker := &checkRecorder{checker: app.FGA}
	app.DB = app.DB.Session(&gorm.Session{Logger: queries})
	app.FGA = checker
	chosen, err := app.ScheduleSummary(ctx, admin, s.ScheduleFilter{})
	must(t, err)
	if len(chosen.Shifts) != 15 {
		t.Fatalf("default loaded %d summaries instead of one stand/day", len(chosen.Shifts))
	}
	defaultSQL, defaultFGA := len(queries.statements), checker.count
	for _, q := range queries.statements {
		if (strings.Contains(q, "JOIN workers") || strings.Contains(q, "FROM workers")) && !strings.HasPrefix(q, "SELECT id FROM workers WHERE account_id=") {
			t.Fatalf("summary fetched worker details: %s", q)
		}
	}
	queries.statements = nil
	checker.count = 0
	all, err := app.ScheduleSummary(ctx, admin, s.ScheduleFilter{Stand: "all", Day: "2027-09-10"})
	must(t, err)
	if len(all.Shifts) != 100 || !all.HasNext {
		t.Fatalf("all-stands page is not bounded: %d, next=%t", len(all.Shifts), all.HasNext)
	}
	if len(queries.statements) > defaultSQL+2 || checker.count > defaultFGA {
		t.Fatalf("read costs grew with shift count: default SQL/FGA %d/%d, all %d/%d", defaultSQL, defaultFGA, len(queries.statements), checker.count)
	}
	aggregates := 0
	for _, q := range queries.statements {
		if strings.Contains(q, "WITH chosen AS") {
			aggregates++
		}
		if (strings.Contains(q, "JOIN workers") || strings.Contains(q, "FROM workers")) && !strings.HasPrefix(q, "SELECT id FROM workers WHERE account_id=") {
			t.Fatalf("summary fetched worker details: %s", q)
		}
	}
	if aggregates != 1 {
		t.Fatalf("expected one bounded aggregate query, got %d", aggregates)
	}
	t.Logf("150 shifts/10 stands: chosen 15 summaries SQL=%d FGA=%d; all-stand bounded100 SQL=%d FGA=%d", defaultSQL, defaultFGA, len(queries.statements), checker.count)

	exec(t, app, s.Command{Action: "edition-save", ID: edition, Label: "Korrigierte Ausgabe", StartDay: "2027-12-10", EndDay: "2027-12-12"})
	for _, filter := range []s.ScheduleFilter{{}, {Stand: "all", Day: "2027-09-10"}} {
		queries.statements = nil
		checker.count = 0
		retained, err := app.ScheduleSummary(ctx, admin, filter)
		must(t, err)
		want := 15
		if filter.Stand == "all" {
			want = 100
		}
		if len(retained.Shifts) != want || retained.HasNext != (want == 100) || len(queries.statements) > defaultSQL+1 || checker.count > defaultFGA {
			t.Fatalf("retained summary costs grew: shifts=%d next=%t SQL/FGA=%d/%d, original=%d/%d", len(retained.Shifts), retained.HasNext, len(queries.statements), checker.count, defaultSQL, defaultFGA)
		}
		aggregates, existence := 0, 0
		for _, q := range queries.statements {
			if strings.Contains(q, "WITH chosen AS") {
				aggregates++
				if !strings.Contains(q, "LIMIT 101 OFFSET 0") {
					t.Fatalf("retained summary aggregate lost its page bound: %s", q)
				}
			}
			if strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM shifts WHERE participation_id IN ") {
				existence++
			}
			if (strings.Contains(q, "JOIN workers") || strings.Contains(q, "FROM workers")) && !strings.HasPrefix(q, "SELECT id FROM workers WHERE account_id=") {
				t.Fatalf("retained summary fetched worker details: %s", q)
			}
		}
		if aggregates != 1 || existence != 1 {
			t.Fatalf("retained summary expected one page query and one existence check: %d/%d", aggregates, existence)
		}
		for _, sh := range retained.Shifts {
			if !sh.Outside {
				t.Fatal("retained summary lost outside marker")
			}
		}
	}
}
