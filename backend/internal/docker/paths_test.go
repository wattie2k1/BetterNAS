package docker

import "testing"

func TestCleanRoot(t *testing.T) {
	cases := []struct {
		in     string
		legacy bool
		want   string
		ok     bool
	}{
		{"/srv/pools/tank/docker/", false, "/srv/pools/tank/docker", true},
		{"/srv/pools", false, "", false},
		{"/srv/disks", false, "", false},
		{"/srv/pools/../etc", false, "", false},
		{"/etc/x", false, "", false},
		{"/", false, "", false},
		{"relativ/pfad", false, "", false},
		{"/srv/appdata", false, "", false},
		{"/srv/appdata", true, "/srv/appdata", true},
		{"/srv/disks/ab/dd", false, "/srv/disks/ab/dd", true},
		{"/srv/pools/Mein Pool/x y", false, "/srv/pools/Mein Pool/x y", true},
		{"/srv/pools/a\x00b", false, "", false},
	}
	for _, c := range cases {
		got, err := CleanRoot(c.in, c.legacy)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("CleanRoot(%q,%v) = %q, %v", c.in, c.legacy, got, err)
		}
	}
}

func TestPathsValidate(t *testing.T) {
	cases := []struct {
		p      Paths
		legacy bool
		ok     bool
	}{
		{Paths{"/srv/pools/a/x", "/srv/pools/a/x"}, false, false},
		{Paths{"/srv/pools/a", "/srv/pools/a/b"}, false, false},
		{Paths{"/srv/pools/a/b", "/srv/pools/a"}, false, false},
		{LegacyPaths, true, true},
		{LegacyPaths, false, false},
		{Paths{"/srv/pools/a/cfg", "/srv/pools/b/dd"}, false, true},
		{Paths{"/srv/pools/a/cfg", "/srv/pools/a/dd"}, false, true},
		{Paths{"", "/srv/pools/b/dd"}, false, false},
	}
	for i, c := range cases {
		if err := c.p.Validate(c.legacy); (err == nil) != c.ok {
			t.Errorf("case %d %+v: %v", i, c.p, err)
		}
	}
}

func TestPathsDirs(t *testing.T) {
	p := Paths{"/srv/pools/a/cfg", "/srv/pools/b/dd"}
	for got, want := range map[string]string{
		p.StacksDir():     "/srv/pools/a/cfg/stacks",
		p.ProxyDir():      "/srv/pools/a/cfg/reverse-proxy",
		p.BackupBase():    "/srv/pools/a/cfg/backups/stacks",
		p.DataRoot():      "/srv/pools/b/dd/docker",
		p.StackDir("web"): "/srv/pools/a/cfg/stacks/web",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestPathsReserved(t *testing.T) {
	p := Paths{"/srv/pools/a/cfg", "/srv/pools/b/dd"}
	for _, c := range []struct {
		root, first string
		want        bool
	}{
		{p.Appdata, "stacks", true}, {p.Appdata, "reverse-proxy", true}, {p.Appdata, "backups", true}, {p.Appdata, "emby", false},
		{p.Dockerdata, "docker", true}, {p.Dockerdata, "stacks", false}, {p.Dockerdata, "emby", false},
		{"/srv/pools/x", "stacks", false},
	} {
		if got := p.Reserved(c.root, c.first); got != c.want {
			t.Errorf("Reserved(%q,%q)=%v", c.root, c.first, got)
		}
	}
	l := LegacyPaths
	for _, n := range []string{"stacks", "docker", "backups", "reverse-proxy"} {
		if !l.Reserved("/srv/appdata", n) {
			t.Errorf("legacy %s", n)
		}
	}
}
