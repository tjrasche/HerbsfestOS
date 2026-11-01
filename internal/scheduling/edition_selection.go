package scheduling

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func (h *Handler) selectEdition(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	edition := r.PostForm.Get("edition")
	target, ok := parseReturn(r.PostForm.Get("return_to"))
	if !ok {
		target = ReturnContext{Path: "/", Query: url.Values{}}
	}
	destination := "/"
	_, err := h.store.readPage(r.Context(), identity(r), edition, func(t *Tx, shell ui.ShellModel) error {
		q := cloneValues(target.Query)
		q.Del("page")
		q.Del("saved")
		q.Del("notice")
		if shell.Global && shell.Selected != "" {
			q.Set("edition", shell.Selected)
		} else {
			q.Del("edition")
		}
		var oldPart Participation
		if p := target.Query.Get("stand"); p != "" && p != "all" {
			var e error
			oldPart, e = t.participation(p)
			if e != nil {
				return e
			}
			if e = t.permitPart(oldPart, "view_schedule"); e != nil {
				return e
			}
		}
		path := target.Path
		segments := strings.Split(strings.Trim(path, "/"), "/")
		if len(segments) > 1 && segments[0] == "shifts" {
			_, p, e := t.shift(segments[1], false)
			if e != nil {
				return e
			}
			if e = t.permitPart(p, "view_schedule"); e != nil {
				return e
			}
			oldPart = p
			path = "/"
		}
		if len(segments) > 2 && segments[0] == "admin" && segments[1] == "editions" && segments[2] != "new" {
			path = "/admin/editions/" + shell.Selected
			if shell.Selected == "" {
				path = "/admin/editions"
			}
			if len(segments) == 5 && segments[3] == "participations" {
				p, e := t.participation(segments[4])
				if e != nil {
					return e
				}
				if p.EditionID != segments[2] {
					return ErrInvalid
				}
				oldPart = p
			}
		}
		q.Del("stand")
		q.Del("day")
		q.Del("shift")
		if nestedRaw := q.Get("return_to"); nestedRaw != "" {
			nested, valid := parseReturn(nestedRaw)
			compatible := valid
			if nestedEdition := nested.Query.Get("edition"); nestedEdition != "" && nestedEdition != shell.Selected {
				compatible = false
			}
			if nestedStand := nested.Query.Get("stand"); nestedStand != "" && nestedStand != "all" {
				p, e := t.participation(nestedStand)
				if e != nil || p.EditionID != shell.Selected {
					compatible = false
				}
			}
			segments := strings.Split(strings.Trim(nested.Path, "/"), "/")
			if len(segments) > 1 && segments[0] == "shifts" {
				_, p, e := t.shift(segments[1], false)
				if e != nil || p.EditionID != shell.Selected {
					compatible = false
				}
			}
			if !compatible {
				q.Del("return_to")
			}
		}
		if oldPart.StandID != "" && shell.Selected != "" {
			var p Participation
			if e := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.edition_id=? AND p.stand_id=?", shell.Selected, oldPart.StandID).Scan(&p).Error; e != nil {
				return e
			}
			if p.ID != "" {
				permission := "view_schedule"
				if path == "/directory" || (strings.HasPrefix(path, "/workers/") && strings.HasSuffix(path, "/assign")) {
					permission = "edit_schedule"
				}
				if e := t.permitPart(p, permission); e == nil {
					q.Set("stand", p.ID)
					if strings.HasPrefix(path, "/admin/editions/") {
						path = "/admin/editions/" + shell.Selected + "/participations/" + p.ID
					}
				} else if !errors.Is(e, ErrDenied) {
					return e
				}
			}
		}
		destination = canonical(path, q)
		return nil
	})
	if err != nil {
		h.problem(w, r, err)
		return
	}
	// Canonical URLs replace the legacy preference cookie.
	http.SetCookie(w, &http.Cookie{Name: "herbstfest_edition", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	h.success(w, r, h.returnTo(r, destination, "/"))
}
