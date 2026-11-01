package scheduling_test

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
)

func TestLinkReplacementHTTPRequiresExplicitConfirmation(t *testing.T) {
	for _, kind := range []string{"shift", "participation", "connection"} {
		for _, hx := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/full", true: "/frame"}[hx], func(t *testing.T) {
				app, db := setup(t)
				edition, part, shift := baseline(t, app)
				web := uiHTTP(t, app, func() { must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer")) })
				path, back := "/shifts/"+shift+"/links", "/shifts/"+shift+"?edition="+edition+"&stand="+part+"&day=2027-09-10"
				command := s.Command{Action: "signup-link", ParticipationID: part, ID: shift}
				if kind == "participation" {
					path = "/participations/" + part + "/links"
					back = "/admin/editions/" + edition + "/participations/" + part
					command.ID = ""
				}
				if kind == "connection" {
					var stand string
					must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
					worker := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Ada", LastName: "Helferin"}).ID
					path = "/workers/" + worker + "/connection-links"
					back = "/workers/" + worker + "?edition=" + edition
					command = s.Command{Action: "connection-link", WorkerID: worker}
				}
				old := exec(t, app, command)
				parsedBack, err := url.Parse(back)
				must(t, err)
				parsedBack.RawQuery = parsedBack.Query().Encode()
				back = parsedBack.String()
				before := count(t, db, "SELECT count(*) FROM capabilities")
				post := func(confirm bool) *httptest.ResponseRecorder {
					values := url.Values{"return_to": {back}}
					if confirm {
						values.Set("confirm", "true")
					}
					r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					if hx {
						r.Header.Set("HX-Request", "true")
					}
					w := httptest.NewRecorder()
					web.ServeHTTP(w, r)
					return w
				}
				missing := post(false)
				if missing.Code != 422 || !strings.Contains(missing.Body.String(), "Es wurde kein Link erstellt oder ersetzt") || !strings.Contains(missing.Body.String(), `id="link-replacement-confirm"`) || !strings.Contains(missing.Body.String(), html.EscapeString(back)) {
					t.Fatal("missing confirmation did not preserve the reconfirmation form/context", missing.Code)
				}
				if count(t, db, "SELECT count(*) FROM capabilities") != before || count(t, db, "SELECT count(*) FROM capabilities WHERE digest=? AND NOT disabled", old.ID) != 1 {
					t.Fatal("unconfirmed HTTP request changed the existing capability")
				}
				if kind == "connection" {
					_, err := app.InspectProof(ctx, old.Token)
					must(t, err)
				} else {
					_, err := app.Signup(ctx, old.Token)
					must(t, err)
				}
				confirmed := post(true)
				if confirmed.Code != 200 || !strings.Contains(confirmed.Body.String(), "Link erstellt") || !strings.Contains(confirmed.Body.String(), "http://127.0.0.1:58081/") {
					t.Fatal("confirmed link replacement failed", confirmed.Code)
				}
				if count(t, db, "SELECT count(*) FROM capabilities") != before+1 || count(t, db, "SELECT count(*) FROM capabilities WHERE digest=? AND disabled", old.ID) != 1 {
					t.Fatal("confirmed replacement did not invalidate exactly the prior capability")
				}
				if kind == "connection" {
					if _, err := app.InspectProof(ctx, old.Token); !errors.Is(err, s.ErrDenied) {
						t.Fatal("old connection proof remained usable", err)
					}
				} else {
					if _, err := app.Signup(ctx, old.Token); !errors.Is(err, s.ErrDenied) {
						t.Fatal("old signup link remained usable", err)
					}
				}
			})
		}
	}
}
