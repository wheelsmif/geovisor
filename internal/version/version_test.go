package version

import "testing"

func TestResolveVersionPrefersInjectedRelease(t *testing.T) {
	t.Parallel()
	if got := resolveVersion("v1.2.3", "v9.9.9"); got != "v1.2.3" {
		t.Fatalf("version = %q, want the injected release", got)
	}
}

func TestResolveVersionUsesModuleVersionForDevBuilds(t *testing.T) {
	t.Parallel()
	if got := resolveVersion("0.0.0-dev", "v1.4.0"); got != "v1.4.0" {
		t.Fatalf("version = %q, want the module version", got)
	}
	if got := resolveVersion("0.0.0-dev", "(devel)"); got != "0.0.0-dev" {
		t.Fatalf("version = %q, want the development placeholder", got)
	}
	if got := resolveVersion("0.0.0-dev", ""); got != "0.0.0-dev" {
		t.Fatalf("version = %q, want the development placeholder", got)
	}
}
