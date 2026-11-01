package ui

import "net/url"

// PageMeta and ShellModel contain presentation state from a freshly authorised
// request. Neither is an authorization grant.
type PageMeta struct {
	Title, Active, Heading, Description string
	Breadcrumbs                         []Breadcrumb
}
type Breadcrumb struct{ Label, Href string }
type EditionOption struct{ ID, Label string }
type ShellModel struct {
	Private, Global, Organizer             bool
	AccountID, WorkerID, Current, Selected string
	CurrentEdition, SelectedEdition        string
	ReturnURL                              string
	Editions                               []EditionOption
}
type FormState struct {
	ID, Summary, Message string
	Values               url.Values
	Errors               map[string]string
}

func (f FormState) Value(name, fallback string) string {
	if values, ok := f.Values[name]; ok {
		if len(values) > 0 {
			return values[0]
		}
		return ""
	}
	return fallback
}
func (f FormState) Error(name string) string { return f.Errors[name] }
func (f FormState) HasErrors() bool          { return f.Summary != "" || len(f.Errors) > 0 }
func (f FormState) Checked(name string) bool { return f.Values.Get(name) == "true" }
func (f FormState) Includes(name, value string) bool {
	for _, v := range f.Values[name] {
		if v == value {
			return true
		}
	}
	return false
}
