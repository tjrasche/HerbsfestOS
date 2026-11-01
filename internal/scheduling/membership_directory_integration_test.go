package scheduling_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

func TestMembershipDirectoryKeepsPersistentStandContextAndMasksOtherPeople(t *testing.T) {
	app, db := setup(t)
	edition, part, _ := baseline(t, app)
	var stand string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	if stand == part {
		t.Fatal("fixture must distinguish persistent stand and participation")
	}
	local := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
	invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part}).Token
	_, err := app.Execute(ctx, local, s.Command{Action: "redeem-organizer", Token: invite})
	must(t, err)
	shell, err := app.Shell(ctx, local, "")
	must(t, err)
	// A second team keeps directory access after the originating stand grant is
	// revoked, so the test specifically exercises the member_stand boundary.
	otherStand := exec(t, app, s.Command{Action: "stand-save", Name: "Anderer Stand"}).ID
	otherPart := s.ID()
	must(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO participations(id,edition_id,stand_id) VALUES(?,?,?)", otherPart, edition, otherStand).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO teams(participation_id,account_id) VALUES(?,?)", otherPart, shell.AccountID).Error
	}))
	for i := 0; i < 27; i++ {
		must(t, db.Exec("INSERT INTO workers(id,first_name,last_name,email,phone,notes) VALUES(?,?,?,?,?,?)", fmt.Sprintf("member-context-%02d", i), "Kontext", fmt.Sprintf("Person%02d", i), fmt.Sprintf("private-%02d@example.invalid", i), fmt.Sprintf("09000%02d", i), "SHARED PRIVATE NOTE").Error)
	}
	must(t, db.Exec("INSERT INTO memberships(stand_id,worker_id) VALUES(?,?)", stand, "member-context-00").Error)
	first, err := app.SearchWorkers(ctx, local, s.DirectoryQuery{Query: "Kontext", MemberStand: stand, Page: 1})
	must(t, err)
	if first.MemberStand.ID != stand || first.MemberStand.Name != "Bierstand" || !first.HasNext || len(first.Rows) != 25 || !first.AlreadyMembers["member-context-00"] || first.AlreadyMembers["member-context-01"] {
		t.Fatal("persistent membership page metadata is wrong")
	}
	for _, row := range first.Rows {
		if row.ID == "member-context-01" && (row.Full || strings.Contains(row.Email, "@example") || row.Notes != "") {
			t.Fatal("membership context disclosed an outsider's contacts")
		}
	}
	var html bytes.Buffer
	must(t, s.DirectoryContent(first, s.FormState{}).Render(ctx, &html))
	if strings.Contains(html.String(), "private-01@example.invalid") || strings.Contains(html.String(), "SHARED PRIVATE NOTE") || strings.Contains(html.String(), "/assign?") || !strings.Contains(html.String(), "Zum Stand hinzufügen") || !strings.Contains(html.String(), "Bereits beim Stand") {
		t.Fatal("contextual directory did not render safe direct membership actions")
	}
	if _, err = app.SearchWorkers(ctx, admin, s.DirectoryQuery{Query: "Kontext", MemberStand: part}); !errors.Is(err, s.ErrDenied) {
		t.Fatal("participation ID was accepted as persistent member_stand", err)
	}
	second, err := app.SearchWorkers(ctx, local, s.DirectoryQuery{Query: "Kontext", MemberStand: stand, Page: 2})
	must(t, err)
	if second.MemberStand.ID != stand || len(second.Rows) != 2 || second.HasNext {
		t.Fatal("membership context was lost on the next page")
	}
	web := uiHTTP(t, app, func() {})
	request := func(method, path string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		web.ServeHTTP(w, r)
		return w
	}
	members := request(http.MethodGet, "/stands/"+stand+"/members?q=Kontext", nil)
	if members.Code != 200 || !strings.Contains(members.Body.String(), "member_stand="+stand) {
		t.Fatal("membership page search link lost its originating stand")
	}
	query := url.Values{"member_stand": {stand}, "q": {"Kontext"}, "page": {"2"}}
	directoryPath := "/directory?" + query.Encode()
	page := request(http.MethodGet, directoryPath, nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `name="member_stand" value="`+stand+`"`) || !strings.Contains(page.Body.String(), `name="return_to"`) {
		t.Fatal("HTTP directory lost membership search/paging context", page.Code)
	}
	for attempt := 0; attempt < 2; attempt++ {
		result := request(http.MethodPost, "/stands/"+stand+"/members?"+query.Encode(), url.Values{"worker_id": {"member-context-26"}, "return_to": {directoryPath}})
		if result.Code != http.StatusSeeOther {
			t.Fatal("direct membership POST failed", result.Code)
		}
		location, err := url.Parse(result.Header().Get("Location"))
		must(t, err)
		if location.Path != "/directory" || location.Query().Get("member_stand") != stand || location.Query().Get("q") != "Kontext" || location.Query().Get("page") != "2" {
			t.Fatal("membership POST left the current search page", location.Path, location.RawQuery)
		}
	}
	if count(t, db, "SELECT count(*) FROM memberships WHERE stand_id=? AND worker_id=?", stand, "member-context-26") != 1 {
		t.Fatal("direct membership retries were not idempotent")
	}
	exec(t, app, s.Command{Action: "team-remove", ParticipationID: part, AccountID: shell.AccountID})
	if _, err = app.SearchWorkers(ctx, local, s.DirectoryQuery{Query: "Kontext"}); err != nil {
		t.Fatal("fixture unexpectedly lost unrelated directory permission", err)
	}
	if _, err = app.SearchWorkers(ctx, local, s.DirectoryQuery{Query: "Kontext", MemberStand: stand}); !errors.Is(err, s.ErrDenied) {
		t.Fatal("revoked stand permission survived in membership context", err)
	}
	denied := request(http.MethodGet, directoryPath, nil)
	if denied.Code != 403 || strings.Contains(denied.Body.String(), "private-25@example.invalid") {
		t.Fatal("revoked membership context did not fail closed", denied.Code)
	}
	denied = request(http.MethodPost, "/stands/"+stand+"/members", url.Values{"worker_id": {"member-context-25"}, "return_to": {directoryPath}})
	if denied.Code != 403 || count(t, db, "SELECT count(*) FROM memberships WHERE stand_id=? AND worker_id=?", stand, "member-context-25") != 0 {
		t.Fatal("revoked membership write was accepted", denied.Code)
	}
}
