package ui

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
	return "Feedback"
}
