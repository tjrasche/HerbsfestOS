package scheduling

import (
	"errors"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/http"
	"strings"
)

func requestMeta(heading, active string) ui.PageMeta {
	return ui.PageMeta{Title: heading + " · HerbstfestOS", Heading: heading, Active: active, Breadcrumbs: []ui.Breadcrumb{{Label: "Dienstplanung", Href: "/"}, {Label: heading}}}
}
func (h *Handler) ownRequestsScreen(w http.ResponseWriter, r *http.Request) {
	h.renderOwnRequests(w, r, savedFeedback(r), 200)
}
func (h *Handler) renderOwnRequests(w http.ResponseWriter, r *http.Request, f FormState, status int) {
	filter := filterRequest(r)
	statusFilter := r.URL.Query().Get("status")
	if r.Method == http.MethodPost {
		if context, ok := parseReturn(r.PostForm.Get("return_to")); ok && statusFilter == "" {
			statusFilter = context.Query.Get("status")
		}
	}
	m, err := h.store.OwnRequests(r.Context(), identity(r), filter.Edition, statusFilter, filter.Page)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, requestMeta("Meine Anfragen", "mine"), m.Shell, OwnRequestsContent(m, f))
}
func (h *Handler) pendingRequestsScreen(w http.ResponseWriter, r *http.Request) {
	h.renderPendingRequests(w, r, savedFeedback(r), 200)
}
func (h *Handler) renderPendingRequests(w http.ResponseWriter, r *http.Request, f FormState, status int) {
	m, err := h.store.PendingRequests(r.Context(), identity(r), filterRequest(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, requestMeta("Offene Anfragen", "pending"), m.Shell, PendingRequestsContent(m, f))
}
func (h *Handler) requestApproveScreen(w http.ResponseWriter, r *http.Request) {
	h.requestDecisionScreen(w, r, "approve")
}
func (h *Handler) requestDeclineScreen(w http.ResponseWriter, r *http.Request) {
	h.requestDecisionScreen(w, r, "decline")
}
func (h *Handler) requestWithdrawScreen(w http.ResponseWriter, r *http.Request) {
	h.requestDecisionScreen(w, r, "withdraw")
}
func (h *Handler) requestDecisionScreen(w http.ResponseWriter, r *http.Request, action string) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	row, shell, err := h.store.requestResource(r.Context(), identity(r), r.PathValue("request"), filterRequest(r).Edition)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	fallback := canonical("/shifts/"+row.ShiftID, contextQuery(shell))
	if action == "withdraw" {
		fallback = "/requests/mine"
	}
	destination := h.returnTo(r, r.PostForm.Get("return_to"), fallback)
	_, err = h.store.Execute(r.Context(), identity(r), Command{Action: action, ID: r.PathValue("request")})
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
			f := formState(r, err)
			if strings.HasPrefix(destination, "/requests/pending") {
				h.renderPendingRequests(w, r, f, errorStatus(err))
			} else if strings.HasPrefix(destination, "/requests/mine") {
				h.renderOwnRequests(w, r, f, errorStatus(err))
			} else {
				h.renderShift(w, r, row.ShiftID, f, errorStatus(err))
			}
			return
		}
		h.problem(w, r, err)
		return
	}
	notice := "decision"
	if action == "withdraw" {
		notice = "withdrawn"
	}
	h.success(w, r, withNotice(destination, notice))
}
func (h *Handler) memberSignupScreen(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	shell, err := h.store.Shell(r.Context(), identity(r), filterRequest(r).Edition)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if shell.WorkerID == "" {
		h.problem(w, r, ErrDenied)
		return
	}
	worker, err := h.store.Worker(r.Context(), identity(r), shell.WorkerID)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	_, err = h.store.Submit(r.Context(), identity(r), SignupInput{Key: r.PostForm.Get("key"), FirstName: worker.FirstName, LastName: worker.LastName, ShiftIDs: r.PostForm["shift"]})
	if err != nil {
		if len(r.PostForm["shift"]) == 1 && (errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict)) {
			h.renderShift(w, r, r.PostForm["shift"][0], formState(r, err), errorStatus(err))
			return
		}
		h.problem(w, r, err)
		return
	}
	destination := h.returnTo(r, r.PostForm.Get("return_to"), "/requests/mine")
	h.success(w, r, withNotice(destination, "requested"))
}
