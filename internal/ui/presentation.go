package ui

import (
	"github.com/a-h/templ"
	"net/url"
	"strings"
)

type navItem struct{ ID, Label, Href string }

func navItems(s ShellModel) []navItem {
	edition := ""
	if s.Global && s.Selected != "" {
		edition = "?edition=" + url.QueryEscape(s.Selected)
	}
	out := []navItem{{"schedule", "Dienstplanung", "/" + edition}, {"mine", "Meine Anfragen", "/requests/mine"}}
	if s.Organizer {
		out = append(out, navItem{"directory", "Helfer suchen", "/directory" + edition}, navItem{"pending", "Offene Anfragen", "/requests/pending" + edition})
	}
	if s.WorkerID != "" {
		out = append(out, navItem{"worker", "Meine Angaben", "/workers/" + s.WorkerID})
	}
	if s.Global {
		out = append(out, navItem{"admin", "Organisation", "/admin" + edition}, navItem{"feedback", "Feedback", "/feedback-notes" + edition})
	}
	return out
}
func LinkAttributes(href string) templ.Attributes {
	if !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") || strings.HasPrefix(href, "/auth/") || strings.HasPrefix(href, "/print/") || strings.HasPrefix(href, "/proof/") || strings.HasPrefix(href, "/register/") {
		return nil
	}
	return templ.Attributes{"hx-get": href, "hx-target": "#app-frame", "hx-swap": "outerHTML", "hx-push-url": "true", "hx-indicator": "#page-loading"}
}
func buttonVariant(v string) string {
	switch v {
	case "primary", "secondary", "quiet", "danger":
		return v
	}
	return "secondary"
}
func editionExplanation(s ShellModel) string {
	if s.Selected != s.Current {
		return "Nicht aktuelle Ausgabe – Dienstplanung nur lesbar. Die Gesamtorganisation kann diese Ausgabe gezielt aktivieren."
	}
	return "Dienstplan und Anfragen dieser Ausgabe"
}
func errorHeading(s int) string {
	switch s {
	case 403:
		return "Hier fehlt die Berechtigung"
	case 404:
		return "Diese Seite gibt es nicht"
	case 409:
		return "Die Daten haben sich geändert"
	case 422:
		return "Bitte prüfe deine Angaben"
	case 503:
		return "Gerade nicht erreichbar"
	}
	return "Die Seite konnte nicht geladen werden"
}

func scheduleHref(s ShellModel) string {
	if s.Global && s.Selected != "" {
		return "/?edition=" + url.QueryEscape(s.Selected)
	}
	return "/"
}
