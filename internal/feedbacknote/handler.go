package feedbacknote

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.list)
	mux.HandleFunc("POST /feedback-notes", h.create)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	h.show(w, r, "", "", http.StatusOK)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	title := r.PostForm.Get("title")
	if err := h.service.Create(r.Context(), title); err != nil {
		if errors.Is(err, ErrInvalidTitle) {
			h.show(w, r, title, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		slog.Error("create note", "error", err)
		http.Error(w, "Unable to create note", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.show(w, r, "", "", http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request, title, message string, status int) {
	notes, err := h.service.List(r.Context())
	if err != nil {
		slog.Error("list notes", "error", err)
		http.Error(w, "Unable to load notes", http.StatusInternalServerError)
		return
	}
	var component templ.Component = Page(notes, title, message)
	if r.Header.Get("HX-Request") == "true" {
		component = Content(notes, title, message)
	}
	w.Header().Set("Vary", "HX-Request")
	w.Header().Set("Cache-Control", "no-store")
	templ.Handler(component, templ.WithStatus(status)).ServeHTTP(w, r)
}
