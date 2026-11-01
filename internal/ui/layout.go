package ui

import (
	"context"
	"github.com/a-h/templ"
	"io"
)

// Layout preserves children for reference/legacy callers. A templ-generated
// wrapper clears its child context before evaluating component arguments, so
// capture children here before Page establishes its own component context.
func Layout(title, activePage string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		children := templ.GetChildren(ctx)
		meta := PageMeta{Title: title, Active: activePage, Heading: sectionTitle(activePage)}
		if referenceFragment(ctx) {
			return Frame(meta, ShellModel{}, children).Render(ctx, w)
		}
		return Page(meta, ShellModel{}, children).Render(ctx, w)
	})
}

func activePageValue(active bool) string {
	if active {
		return "page"
	}
	return "false"
}

func sectionTitle(page string) string {
	if page == "access-denied" {
		return "Zugang"
	}
	if page == "design-system" {
		return "Designsystem"
	}
	if page == "feedback" {
		return "Feedback"
	}
	return "Dienstplanung"
}
