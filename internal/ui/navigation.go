package ui

import "context"

type navigationKey struct{}
type Navigation struct{ Private, Global, Organizer bool }

func WithNavigation(ctx context.Context, n Navigation) context.Context {
	return context.WithValue(ctx, navigationKey{}, n)
}
func navigation(ctx context.Context) Navigation {
	n, _ := ctx.Value(navigationKey{}).(Navigation)
	return n
}
