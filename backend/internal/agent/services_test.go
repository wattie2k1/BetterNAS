package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

const managedSmb = "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n[global]\n   workgroup = WORKGROUP\n   server min protocol = SMB2_10\n   include = /etc/samba/betternas.d/shares.conf\n"

func TestServiceActionWhitelist(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	if err := ServiceAction(ctx, d, "docker", "restart"); err != nil || !f.Called("systemctl restart docker") {
		t.Fatal(err, f.Calls)
	}
	for _, c := range [][2]string{{"sshd; reboot", "start"}, {"cron", "stop"}, {"docker", "disable"}, {"docker", "restart --now"}} {
		if code(ServiceAction(ctx, d, c[0], c[1])) != apierr.InvalidParams {
			t.Errorf("%v muss abgelehnt werden", c)
		}
	}
}

func TestListServices(t *testing.T) {
	d, f, _ := vmEnv(t)
	f.Responses["systemctl is-active smbd"] = sysexec.Result{Stdout: []byte("active\n")}
	f.Responses["systemctl is-enabled smbd"] = sysexec.Result{Stdout: []byte("enabled\n")}
	f.Responses["systemctl is-active nmbd"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	f.Responses["systemctl is-enabled nmbd"] = sysexec.Result{ExitCode: 1, Stdout: []byte("not-found\n")}
	list, _ := ListServices(context.Background(), d)
	if len(list) != len(Services) || !list[0].Active || !list[0].Enabled || !list[0].Installed || list[1].Active || list[1].Installed {
		t.Fatalf("%+v", list[:2])
	}
}

func TestSetSMBGlobal(t *testing.T) {
	d, f, root := vmEnv(t)
	writeFile(t, root, "/etc/samba/smb.conf")
	_ = d.FS.WriteFile("/etc/samba/smb.conf", []byte(managedSmb), 0o644)
	f.Responses["systemctl is-active smbd"] = sysexec.Result{Stdout: []byte("active\n")}
	if err := SetSMBGlobal(context.Background(), d, SMBGlobal{Workgroup: "heim", ServerName: "Mein NAS", MinProtocol: "SMB3_11"}); err != nil {
		t.Fatal(err)
	}
	conf := read(t, root, "etc/samba/smb.conf")
	if i, j := strings.Index(conf, "global.conf"), strings.Index(conf, "shares.conf"); i < 0 || j < 0 || i > j {
		t.Fatal("global.conf muss vor shares.conf eingebunden sein:\n" + conf)
	}
	if g := read(t, root, "etc/samba/betternas.d/global.conf"); !strings.Contains(g, "workgroup = HEIM") || !strings.Contains(g, "server string = Mein NAS") || !strings.Contains(g, "SMB3_11") {
		t.Fatal(g)
	}
	if !f.Called("testparm -s") || !f.Called("smbcontrol smbd reload-config") {
		t.Fatal(f.Calls)
	}
	// zweites Mal: Include nicht doppelt
	if err := SetSMBGlobal(context.Background(), d, SMBGlobal{Workgroup: "HEIM", ServerName: "x", MinProtocol: "SMB2_10"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(read(t, root, "etc/samba/smb.conf"), "global.conf") != 1 {
		t.Fatal("Include doppelt")
	}
	if got := GetSMBGlobal(d); got.Workgroup != "HEIM" || got.ServerName != "x" {
		t.Fatalf("%+v", got)
	}
}

func TestSetSMBGlobalRollbackAndValidation(t *testing.T) {
	d, f, root := vmEnv(t)
	writeFile(t, root, "/etc/samba/smb.conf")
	_ = d.FS.WriteFile("/etc/samba/smb.conf", []byte(managedSmb), 0o644)
	f.Responses["testparm -s"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if code(SetSMBGlobal(context.Background(), d, SMBGlobal{Workgroup: "A", ServerName: "x", MinProtocol: "SMB2_10"})) != apierr.InvalidParams {
		t.Fatal("testparm-Fehler")
	}
	if read(t, root, "etc/samba/smb.conf") != managedSmb || d.FS.Exists("/etc/samba/betternas.d/global.conf") {
		t.Fatal("Rollback")
	}
	for name, g := range map[string]SMBGlobal{
		"wg":    {Workgroup: "A B", ServerName: "x", MinProtocol: "SMB2_10"},
		"name":  {Workgroup: "A", ServerName: "x\n[evil]", MinProtocol: "SMB2_10"},
		"name2": {Workgroup: "A", ServerName: "a%U", MinProtocol: "SMB2_10"},
		"proto": {Workgroup: "A", ServerName: "x", MinProtocol: "NT1"},
	} {
		if code(SetSMBGlobal(context.Background(), d, g)) != apierr.InvalidParams {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
	// nicht verwaltete smb.conf wird nicht angefasst
	_ = d.FS.WriteFile("/etc/samba/smb.conf", []byte("[global]\n"), 0o644)
	f.Responses["testparm -s"] = sysexec.Result{}
	if code(SetSMBGlobal(context.Background(), d, SMBGlobal{Workgroup: "A", ServerName: "x", MinProtocol: "SMB2_10"})) != apierr.Conflict {
		t.Fatal("unverwaltet")
	}
}

func TestDockerSettings(t *testing.T) {
	d, f, root := vmEnv(t)
	writeFile(t, root, "/etc/docker/daemon.json")
	_ = d.FS.WriteFile("/etc/docker/daemon.json", []byte(`{"data-root":"/srv/appdata/docker","log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"},"dns":["1.1.1.1"]}`), 0o644)
	got, err := GetDockerSettings(d)
	if err != nil || got.DataRoot != "/srv/appdata/docker" || got.LogMaxSize != "10m" || got.LogMaxFile != 3 || got.LiveRestore {
		t.Fatalf("%+v %v", got, err)
	}
	err = SetDockerSettings(context.Background(), d, DockerSettings{LogMaxSize: "50m", LogMaxFile: 5, RegistryMirrors: []string{"https://mirror.example.org"}, LiveRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	out := read(t, root, "etc/docker/daemon.json")
	for _, want := range []string{`"max-size": "50m"`, `"max-file": "5"`, `"registry-mirrors"`, `"live-restore": true`, `"data-root": "/srv/appdata/docker"`, `"dns"`} {
		if !strings.Contains(out, want) {
			t.Errorf("%s fehlt:\n%s", want, out)
		}
	}
	if !f.Called("dockerd --validate --config-file=/etc/docker/daemon.json") || f.Called("systemctl restart docker") {
		t.Fatal("prüfen ja, neu starten nein", f.Calls)
	}
}

func TestDockerSettingsRollbackAndValidation(t *testing.T) {
	d, f, root := vmEnv(t)
	writeFile(t, root, "/etc/docker/daemon.json")
	orig := `{"data-root":"/x"}`
	_ = d.FS.WriteFile("/etc/docker/daemon.json", []byte(orig), 0o644)
	f.Responses["dockerd --validate"] = sysexec.Result{ExitCode: 1, Stderr: []byte("nope")}
	ok := DockerSettings{LogMaxSize: "10m", LogMaxFile: 3}
	if code(SetDockerSettings(context.Background(), d, ok)) != apierr.InvalidParams || read(t, root, "etc/docker/daemon.json") != orig {
		t.Fatal("Rollback")
	}
	f.Responses["dockerd --validate"] = sysexec.Result{}
	for name, s := range map[string]DockerSettings{
		"größe":   {LogMaxSize: "10", LogMaxFile: 3},
		"größe2":  {LogMaxSize: "10m; reboot", LogMaxFile: 3},
		"datei":   {LogMaxSize: "10m", LogMaxFile: 0},
		"mirror":  {LogMaxSize: "10m", LogMaxFile: 3, RegistryMirrors: []string{"ftp://x"}},
		"mirror2": {LogMaxSize: "10m", LogMaxFile: 3, RegistryMirrors: []string{"https://user:pw@x.de"}},
	} {
		if code(SetDockerSettings(context.Background(), d, s)) != apierr.InvalidParams {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
	_ = d.FS.WriteFile("/etc/docker/daemon.json", []byte("{kaputt"), 0o644)
	if code(SetDockerSettings(context.Background(), d, ok)) != apierr.Conflict {
		t.Fatal("kaputte JSON wird nicht überschrieben")
	}
}
