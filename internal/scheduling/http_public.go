package scheduling

import (
	"errors"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/http"
)

func publicMeta(heading string) ui.PageMeta {
	return ui.PageMeta{Title: heading + " · HerbstfestOS", Active: "signup", Heading: heading}
}
func (h *Handler) publicSignupScreen(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	in := SignupInput{Token: token, Key: freshKey()}
	f := FormState{ID: "signup", Errors: map[string]string{}}
	statusCode := 200
	if r.Method == http.MethodPost {
		if err := parse(w, r); err != nil {
			h.problem(w, r, err)
			return
		}
		in.Key = r.PostForm.Get("key")
		in.FirstName = r.PostForm.Get("first_name")
		in.LastName = r.PostForm.Get("last_name")
		in.ShiftIDs = r.PostForm["shift"]
		f.Values = r.PostForm
		if !textValid(in.FirstName, 120) {
			f.Errors["first_name"] = "Bitte deinen Vornamen mit höchstens 120 Zeichen angeben."
		}
		if !textValid(in.LastName, 120) {
			f.Errors["last_name"] = "Bitte deinen Nachnamen mit höchstens 120 Zeichen angeben."
		}
		if len(in.ShiftIDs) == 0 {
			f.Errors["shift"] = "Bitte mindestens einen Dienst auswählen."
		}
		if len(f.Errors) == 0 {
			out, err := h.store.Submit(r.Context(), identity(r), in)
			if err == nil {
				h.respond(w, r, 200, publicMeta("Anfragen gespeichert"), ui.ShellModel{}, SubmissionReceiptContent(out))
				return
			}
			if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) {
				h.problem(w, r, err)
				return
			}
			statusCode = errorStatus(err)
			f.Summary = "Nichts aus dieser Anfrage wurde gespeichert. Bitte prüfe die markierten Dienste und versuche es erneut."
			f.Errors["shift"] = "Ein Dienst ist möglicherweise inzwischen voll oder nicht mehr verfügbar. Bitte Auswahl prüfen."
		} else {
			statusCode = 422
			f.Summary = "Nichts gespeichert. Bitte die markierten Felder ergänzen."
		}
	}
	v, err := h.store.Signup(r.Context(), token)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, statusCode, publicMeta("Zum Helfen anmelden"), ui.ShellModel{}, SignupContent(v, in, f, h.store.Now()))
}
func (h *Handler) proofScreen(w http.ResponseWriter, r *http.Request) {
	h.renderProof(w, r, FormState{}, 200)
}
func (h *Handler) renderProof(w http.ResponseWriter, r *http.Request, f FormState, statusCode int) {
	v, err := h.store.ProofContext(r.Context(), r.PathValue("token"), identity(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, statusCode, publicMeta("Private Einladung bestätigen"), ui.ShellModel{}, ProofContextContent(v, r.PathValue("token"), f))
}
func (h *Handler) proofAcceptScreen(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	proof, err := h.store.ProofContext(r.Context(), r.PathValue("token"), identity(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if r.PostForm.Get("confirm") != "true" {
		f := formState(r, ErrInvalid)
		f.Summary = "Bitte die Verbindung für dein angemeldetes Konto ausdrücklich bestätigen."
		h.renderProof(w, r, f, 422)
		return
	}
	action := "connect"
	if proof.Kind == "organizer" {
		action = "redeem-organizer"
	}
	_, err = h.store.Execute(r.Context(), identity(r), Command{Action: action, Token: r.PathValue("token")})
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.success(w, r, withNotice("/", "saved"))
}
