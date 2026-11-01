package scheduling

import "testing"

func TestReturnContextRejectsUntrustedRoutesAndCanonicalizesFilters(t *testing.T) {
	for _, value := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example/", "/auth/login", "/actions", "/proof/secret", "/register/secret", "/admin/editions/x/activate", "/?day=2027-02-30", "/directory?q=%0aLocation:evil", "/%2f%2fevil.example/"} {
		if got, ok := parseReturn(value); ok {
			t.Errorf("unsafe return %q accepted: %#v", value, got)
		}
	}
	if _, ok := parseReturn("/directory?return_to=%2Fproof%2Fsecret"); ok {
		t.Fatal("nested bearer return accepted")
	}
	got, ok := parseReturn("/directory?q=Anna&page=2&unknown=discard")
	if !ok || got.URL() != "/directory?page=2&q=Anna" {
		t.Fatalf("canonical return: %#v %v", got, ok)
	}
}

func TestBearerLinkUsesOnlyConfiguredTrustedOrigin(t *testing.T) {
	for _, origin := range []string{"", "http://evil.example", "https://app.example/path", "https://person@app.example", "https://app.example?forward=evil"} {
		if value, err := NewHandler(nil, origin).absoluteLink("/proof/private"); err == nil {
			t.Errorf("untrusted origin %q accepted: %s", origin, value)
		}
	}
	for _, origin := range []string{"https://app.example", "http://127.0.0.1:58081", "http://localhost:58081"} {
		value, err := NewHandler(nil, origin).absoluteLink("/proof/private")
		if err != nil || value != origin+"/proof/private" {
			t.Fatalf("configured origin failed: %s %v", value, err)
		}
	}
}
