package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/sysexec"
)

func proxyCfg() docker.ProxyConfig {
	return docker.ProxyConfig{Enabled: true, Email: "a@example.de", HTTPPort: 8080, HTTPSPort: 8443,
		Sites: []docker.ProxySite{{Domain: "jf.example.de", Port: 8096}}}
}

func TestProxyWriteCreatesStackAndPendingCaddyfile(t *testing.T) {
	d, _, root := dockerEnv(t)
	if err := ProxyWrite(context.Background(), d, proxyCfg()); err != nil {
		t.Fatal(err)
	}
	compose, err := d.FS.ReadFile("/srv/appdata/stacks/reverse-proxy/compose.yaml")
	if err != nil || !strings.Contains(string(compose), `"8443:443"`) || !strings.Contains(string(compose), "image: caddy:2") {
		t.Fatalf("compose: %s %v", compose, err)
	}
	cf, err := d.FS.ReadFile("/srv/appdata/reverse-proxy/conf/Caddyfile.new")
	if err != nil || !strings.Contains(string(cf), "jf.example.de {") || !strings.Contains(string(cf), "email a@example.de") {
		t.Fatalf("Caddyfile.new: %s %v", cf, err)
	}
	for _, dir := range []string{"conf", "data", "config"} {
		if !d.FS.Exists("/srv/appdata/reverse-proxy/" + dir) {
			t.Errorf("Verzeichnis %s fehlt", dir)
		}
	}
	// Die laufende Konfiguration bleibt bis zur Übernahme unangetastet.
	if d.FS.Exists("/srv/appdata/reverse-proxy/conf/Caddyfile") {
		t.Error("Caddyfile darf erst nach der Prüfung entstehen")
	}
	_ = root
}

func TestProxyWriteRejectsInvalidConfigAndWritesNothing(t *testing.T) {
	d, _, _ := dockerEnv(t)
	bad := proxyCfg()
	bad.Sites[0].Domain = "x.example.de {\n\treverse_proxy evil:1\n}\nb"
	if err := ProxyWrite(context.Background(), d, bad); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if d.FS.Exists("/srv/appdata/reverse-proxy/conf/Caddyfile.new") || d.FS.Exists("/srv/appdata/stacks/reverse-proxy/compose.yaml") {
		t.Fatal("trotz ungültiger Konfiguration geschrieben")
	}
}

func TestProxyValidateUsesCaddyAndReportsErrors(t *testing.T) {
	d, f, _ := dockerEnv(t)
	_ = ProxyWrite(context.Background(), d, proxyCfg())
	if err := ProxyValidate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	want := "docker run --rm -v /srv/appdata/reverse-proxy/conf:/etc/caddy:ro caddy:2 caddy validate --config /etc/caddy/Caddyfile.new --adapter caddyfile"
	if !f.Called(want) {
		t.Fatalf("%v", f.Calls)
	}
	f.Responses[want] = sysexec.Result{ExitCode: 1, Stderr: []byte("Error: adapting config using caddyfile: Caddyfile:5: unrecognized directive: foo\n")}
	err := ProxyValidate(context.Background(), d)
	if code(err) != apierr.InvalidParams || !strings.Contains(err.Error(), "unrecognized directive") {
		t.Fatalf("%v", err)
	}
	if d.FS.Exists("/srv/appdata/reverse-proxy/conf/Caddyfile.new") {
		t.Fatal("abgelehnte Konfiguration bleibt liegen")
	}
}

func TestProxyCommitReplacesCaddyfile(t *testing.T) {
	d, _, _ := dockerEnv(t)
	ctx := context.Background()
	_ = ProxyWrite(ctx, d, proxyCfg())
	_ = d.FS.WriteFile("/srv/appdata/reverse-proxy/conf/Caddyfile", []byte("alt"), 0o644)
	if err := ProxyCommit(ctx, d); err != nil {
		t.Fatal(err)
	}
	b, err := d.FS.ReadFile("/srv/appdata/reverse-proxy/conf/Caddyfile")
	if err != nil || !strings.Contains(string(b), "jf.example.de") {
		t.Fatalf("%s %v", b, err)
	}
	if d.FS.Exists("/srv/appdata/reverse-proxy/conf/Caddyfile.new") {
		t.Fatal(".new bleibt")
	}
	if err := ProxyCommit(ctx, d); code(err) != apierr.NotFound {
		t.Fatalf("ohne neue Datei: %v", err)
	}
}

func TestProxyReload(t *testing.T) {
	d, f, _ := dockerEnv(t)
	if err := ProxyReload(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.Called("docker exec reverse-proxy-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile") {
		t.Fatalf("%v", f.Calls)
	}
	f.Responses["docker exec reverse-proxy-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile"] = sysexec.Result{ExitCode: 1, Stderr: []byte("no such container")}
	if err := ProxyReload(context.Background(), d); err == nil {
		t.Fatal("Fehler erwartet")
	}
}

func TestCaddyErrorsAreReadable(t *testing.T) {
	jsonOut := `{"level":"info","ts":1,"msg":"using config from file","file":"/etc/caddy/Caddyfile.new"}
{"level":"error","ts":2,"msg":"adapting config using caddyfile: /etc/caddy/Caddyfile.new:2: unrecognized directive: foo"}
`
	if got := caddyErrors(jsonOut); got != "Zeile 2: unrecognized directive: foo" {
		t.Errorf("JSON: %q", got)
	}
	if got := caddyErrors("Error: adapting config using caddyfile: boom\n"); got != "adapting config using caddyfile: boom" {
		t.Errorf("Text: %q", got)
	}
	if got := caddyErrors("irgendwas Unerwartetes"); got != "irgendwas Unerwartetes" {
		t.Errorf("Fallback: %q", got)
	}
}
