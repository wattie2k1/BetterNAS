package docker

import "testing"

func TestCleanFolder(t *testing.T) {
	ok := map[string]string{
		"":                       "",
		"/srv/pools":             "/srv/pools",
		"/srv/pools/":            "/srv/pools",
		"/srv/pools/tank/Medien": "/srv/pools/tank/Medien",
		"/srv/appdata//jf/./cfg": "/srv/appdata/jf/cfg",
		"/srv/appdata":           "/srv/appdata",
	}
	for in, want := range ok {
		if got, err := CleanFolder(in); err != nil || got != want {
			t.Errorf("CleanFolder(%q) = %q, %v; erwartet %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"/", "/etc", "/srv", "/srv/poolsx", "/srv/pools/../../etc", "/srv/pools/a/../../../etc", "srv/pools", "pools",
		"/srv/pools/a\x00b", "/srv/appdata/../pools2", "/srv/pools/..",
	} {
		if got, err := CleanFolder(bad); err == nil {
			t.Errorf("CleanFolder(%q) = %q, sollte abgelehnt werden", bad, got)
		}
	}
}
