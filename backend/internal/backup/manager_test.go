package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

type fakeAgent struct {
	mu      sync.Mutex
	calls   []string
	params  []any
	err     error
	hook    func(method string)
	results map[string]any // Antwort je Methode
	// onCall kann je Methode zusätzlich einen Fehler liefern.
	onCall func(method string, params any) error
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, params)
	if f.hook != nil {
		f.hook(method)
	}
	if f.onCall != nil {
		if err := f.onCall(method, params); err != nil {
			return err
		}
	}
	if r, ok := f.results[method]; ok && result != nil && f.err == nil {
		b, _ := json.Marshal(r)
		_ = json.Unmarshal(b, result)
	}
	return f.err
}

type mgrEnv struct {
	m     *Manager
	agent *fakeAgent
	root  string
	ctx   context.Context
}

func newMgr(t *testing.T) *mgrEnv {
	t.Helper()
	d := testDB(t)
	root := t.TempDir()
	a := &fakeAgent{}
	m := NewManager(d, a, filepath.Join(root, "backups"), "0.5.0-test", events.NewHub())
	return &mgrEnv{m: m, agent: a, root: root, ctx: context.Background()}
}

func (e *mgrEnv) actions(t *testing.T) []string {
	t.Helper()
	list, err := audit.List(e.ctx, e.m.db, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range list {
		out = append(out, a.Action+":"+a.Target+":"+a.Actor)
	}
	return out
}

func TestManagerCreateListDeleteAudit(t *testing.T) {
	e := newMgr(t)
	info, err := e.m.Create(e.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.m.List(e.ctx)
	if err != nil || len(l) != 1 || l[0].Name != info.Name {
		t.Fatalf("%+v %v", l, err)
	}
	if err := e.m.Delete(e.ctx, "anna", info.Name); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.m.List(e.ctx); len(l) != 0 {
		t.Fatalf("%+v", l)
	}
	got := strings.Join(e.actions(t), " ")
	if !strings.Contains(got, "backup.create:"+info.Name+":anna") || !strings.Contains(got, "backup.delete:"+info.Name+":anna") {
		t.Fatalf("Audit: %s", got)
	}
}

func TestManagerCreatePrunesToSeven(t *testing.T) {
	e := newMgr(t)
	for i := 0; i < 9; i++ {
		if _, err := e.m.Create(e.ctx, "anna"); err != nil {
			t.Fatal(err)
		}
	}
	if l, _ := e.m.List(e.ctx); len(l) != 7 {
		t.Fatalf("%d Backups statt 7", len(l))
	}
}

func TestManagerOpenRejectsBadName(t *testing.T) {
	e := newMgr(t)
	for _, n := range []string{"../etc/passwd", "x.tar.gz", ""} {
		if _, err := e.m.Open(e.ctx, n); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", n, err)
		}
	}
}

func TestRestoreNeedsValidToken(t *testing.T) {
	e := newMgr(t)
	if err := e.m.Restore(e.ctx, "anna", "gibtsnicht"); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	info, _ := e.m.Create(e.ctx, "anna")
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil {
		t.Fatal(err)
	}
	e.m.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if err := e.m.Restore(e.ctx, "anna", p.Token); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("abgelaufen: %v", err)
	}
	if len(e.agent.calls) != 0 {
		t.Fatalf("Agent aufgerufen: %v", e.agent.calls)
	}
}

func TestRestoreStagesThenRestarts(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil || p.Manifest.Counts["pools"] != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	var stagedAtCall bool
	e.agent.hook = func(string) {
		_, err := os.Stat(filepath.Join(e.root, "restore", "pending.db"))
		stagedAtCall = err == nil
	}
	if err := e.m.Restore(e.ctx, "anna", p.Token); err != nil {
		t.Fatal(err)
	}
	if !stagedAtCall || len(e.agent.calls) != 1 || e.agent.calls[0] != "system.restart-api" {
		t.Fatalf("staged=%v calls=%v", stagedAtCall, e.agent.calls)
	}
	if !strings.Contains(strings.Join(e.actions(t), " "), "backup.restore:"+info.Name) && !strings.Contains(strings.Join(e.actions(t), " "), "backup.restore") {
		t.Fatalf("kein Audit: %v", e.actions(t))
	}
	// Token ist verbraucht
	if err := e.m.Restore(e.ctx, "anna", p.Token); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestRestoreAgentFailureUnstages(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	p, _ := e.m.PreviewStored(e.ctx, info.Name)
	e.agent.err = errors.New("systemd weg")
	if err := e.m.Restore(e.ctx, "anna", p.Token); err == nil {
		t.Fatal("Fehler erwartet")
	}
	for _, n := range []string{"pending.db", "pending.json"} {
		if _, err := os.Stat(filepath.Join(e.root, "restore", n)); err == nil {
			t.Errorf("%s blieb liegen", n)
		}
	}
}

func TestPreviewUploadRejectsGarbage(t *testing.T) {
	e := newMgr(t)
	_, err := e.m.PreviewUpload(e.ctx, strings.NewReader("das ist kein backup"))
	if apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	// nichts übrig, laufende DB unverändert
	ents, _ := os.ReadDir(filepath.Join(e.root, "restore"))
	for _, en := range ents {
		t.Errorf("Rest im Restore-Verzeichnis: %s", en.Name())
	}
	var n int
	e.m.db.QueryRow(`SELECT COUNT(*) FROM pools`).Scan(&n)
	if n != 1 {
		t.Fatal("DB verändert")
	}
}

func TestPreviewWarnsOnOtherVersion(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	e.m.version = "0.6.0"
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil || len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "0.5.0-test") {
		t.Fatalf("%+v %v", p, err)
	}
}

// customDir richtet einen eigenen Sicherungsordner ein. Der Fake-Agent verhält sich wie der echte:
// backup.store verschiebt das Archiv aus dem Staging in den Zielordner, backup.delete/prune räumen dort auf.
func customDir(t *testing.T, e *mgrEnv) string {
	t.Helper()
	dirCheck = func(p string) (string, error) { return filepath.Clean(p), nil }
	t.Cleanup(func() { dirCheck = CleanDir })
	dst := filepath.Join(e.root, "ziel")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	e.agent.results = map[string]any{"backup.prepare": target{Dir: dst, Free: 1 << 30, Appdata: "/srv/appdata"}}
	e.agent.onCall = func(method string, params any) error {
		p, _ := params.(map[string]any)
		str := func(k string) string {
			if v, ok := params.(map[string]string); ok {
				return v[k]
			}
			s, _ := p[k].(string)
			return s
		}
		switch method {
		case "backup.store":
			return os.Rename(str("src"), filepath.Join(str("dir"), filepath.Base(str("src"))))
		case "backup.delete":
			return Delete(str("dir"), str("name"))
		case "backup.prune":
			keep, _ := p["keep"].(int)
			return Prune(str("dir"), keep)
		case "backup.appdata":
			return os.WriteFile(filepath.Join(str("dir"), "betternas-appdata-20261120-023000.tar.gz"), []byte("tar"), 0o640)
		}
		return nil
	}
	e.agent.results["backup.appdata"] = map[string]any{"name": "betternas-appdata-20261120-023000.tar.gz", "size": 3}
	if err := e.m.SetConfig(e.ctx, "anna", Config{Enabled: true, Time: "02:30", Keep: 2, Dir: dst, Appdata: true}); err != nil {
		t.Fatal(err)
	}
	e.agent.calls = nil
	return dst
}

func TestCustomDirCreateListOpenDelete(t *testing.T) {
	e := newMgr(t)
	dst := customDir(t, e)
	info, err := e.m.Create(e.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, info.Name)); err != nil {
		t.Fatalf("Archiv nicht im Zielordner: %v", err)
	}
	if ents, _ := os.ReadDir(filepath.Join(e.root, "backups")); len(ents) != 0 {
		t.Fatalf("Archiv liegt auch im Standardordner: %v", ents)
	}
	if ents, _ := os.ReadDir(filepath.Join(e.root, "staging")); len(ents) != 0 {
		t.Fatalf("Staging nicht leer: %v", ents)
	}
	l, err := e.m.List(e.ctx)
	if err != nil || len(l) != 1 || l[0].Name != info.Name || l[0].Kind != KindConfig {
		t.Fatalf("%+v %v", l, err)
	}
	f, err := e.m.Open(e.ctx, info.Name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := e.m.Delete(e.ctx, "anna", info.Name); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.m.List(e.ctx); len(l) != 0 {
		t.Fatalf("%+v", l)
	}
	if got := strings.Join(e.agent.calls, " "); got != "backup.store backup.prune backup.delete" {
		t.Fatalf("Agent-Aufrufe: %s", got)
	}
	if err := e.m.Delete(e.ctx, "anna", "../x"); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}

func TestCustomDirStoreFailureLeavesNothing(t *testing.T) {
	e := newMgr(t)
	customDir(t, e)
	e.agent.onCall = func(method string, _ any) error {
		if method == "backup.store" {
			return errors.New("Platte voll")
		}
		return nil
	}
	if _, err := e.m.Create(e.ctx, "anna"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if ents, _ := os.ReadDir(filepath.Join(e.root, "staging")); len(ents) != 0 {
		t.Fatalf("Staging nicht aufgeräumt: %v", ents)
	}
}

func TestCreateAppdata(t *testing.T) {
	e := newMgr(t)
	dst := customDir(t, e)
	for i := 0; i < 3; i++ { // Aufbewahrung 2 gilt je Art
		if _, err := e.m.Create(e.ctx, "anna"); err != nil {
			t.Fatal(err)
		}
		e.m.now = func() time.Time { return time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC) }
	}
	res, err := e.m.CreateAppdata(e.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if res.Info.Kind != KindAppdata || res.Info.Name != "betternas-appdata-20261120-023000.tar.gz" || res.Info.Size != 3 {
		t.Fatalf("%+v", res)
	}
	l, _ := List(dst)
	kinds := map[string]int{}
	for _, i := range l {
		kinds[i.Kind]++
	}
	if kinds[KindConfig] != 2 || kinds[KindAppdata] != 1 {
		t.Fatalf("%v", kinds)
	}
	if !strings.Contains(strings.Join(e.actions(t), " "), "backup.appdata:betternas-appdata-20261120-023000.tar.gz:anna") {
		t.Fatalf("Audit: %v", e.actions(t))
	}
	// appdata-Sicherungen lassen sich nicht über die Konfigurations-Wiederherstellung einspielen
	if _, err := e.m.PreviewStored(e.ctx, res.Info.Name); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}

func TestCreateAppdataOnSystemDiskUsesDefaultDir(t *testing.T) {
	e := newMgr(t)
	e.agent.results = map[string]any{"backup.appdata": map[string]any{"name": "betternas-appdata-20261120-023000.tar.gz", "size": 1}}
	if _, err := e.m.CreateAppdata(e.ctx, "anna"); err != nil {
		t.Fatal(err)
	}
	p, _ := e.agent.params[0].(map[string]string)
	if e.agent.calls[0] != "backup.appdata" || p["dir"] != filepath.Join(e.root, "backups") {
		t.Fatalf("%v %v", e.agent.calls, e.agent.params)
	}
}

func TestCreateAppdataRunsOnlyOnce(t *testing.T) {
	e := newMgr(t)
	started, release := make(chan struct{}), make(chan struct{})
	e.agent.onCall = func(method string, _ any) error {
		if method == "backup.appdata" {
			close(started)
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { _, err := e.m.CreateAppdata(e.ctx, "anna"); done <- err }()
	<-started
	if _, err := e.m.CreateAppdata(e.ctx, "anna"); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("zweiter Lauf: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSetConfigChecksTargetAndAppdata(t *testing.T) {
	e := newMgr(t)
	dst := customDir(t, e)
	good := Config{Enabled: true, Time: "02:30", Keep: 7, Dir: dst}
	// Agent kann den Ordner nicht vorbereiten
	e.agent.onCall = func(method string, _ any) error {
		if method == "backup.prepare" {
			return apierr.New(apierr.InvalidParams, "keine Schreibrechte")
		}
		return nil
	}
	if err := e.m.SetConfig(e.ctx, "anna", good); err == nil || !strings.Contains(err.Error(), "keine Schreibrechte") {
		t.Fatalf("%v", err)
	}
	if c, _ := e.m.Config(e.ctx); c.Keep != 2 {
		t.Fatalf("fehlgeschlagene Einstellung wurde gespeichert: %+v", c)
	}
	// API kann den Ordner nicht lesen
	e.agent.onCall = nil
	e.agent.results["backup.prepare"] = target{Dir: filepath.Join(e.root, "gibtsnicht"), Appdata: "/srv/appdata"}
	if err := e.m.SetConfig(e.ctx, "anna", good); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	// Docker nicht eingerichtet → appdata nicht möglich
	e.agent.results["backup.prepare"] = target{Dir: dst}
	if err := e.m.SetConfig(e.ctx, "anna", Config{Enabled: true, Time: "02:30", Keep: 7, Dir: dst, Appdata: true}); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if err := e.m.SetConfig(e.ctx, "anna", good); err != nil {
		t.Fatal(err)
	}
	// zurück auf die Systemplatte ohne appdata: kein Agent-Aufruf nötig
	e.agent.calls = nil
	if err := e.m.SetConfig(e.ctx, "anna", Config{Enabled: true, Time: "02:30", Keep: 7}); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.calls) != 0 {
		t.Fatalf("%v", e.agent.calls)
	}
}

func TestStatus(t *testing.T) {
	e := newMgr(t)
	e.agent.results = map[string]any{"backup.prepare": target{Dir: "/x", Free: 123, Appdata: "/srv/appdata"}}
	st, err := e.m.Status(e.ctx)
	if err != nil || !st.Default || st.Dir != filepath.Join(e.root, "backups") || st.Free != 123 || st.Appdata != "/srv/appdata" || st.Problem != "" {
		t.Fatalf("%+v %v", st, err)
	}
	e.agent.err = apierr.New(apierr.NotFound, "Ordner weg")
	st, _ = e.m.Status(e.ctx)
	if st.Problem != "Ordner weg" {
		t.Fatalf("%+v", st)
	}
}

func TestRunDailyAlsoBacksUpAppdata(t *testing.T) {
	e := newMgr(t)
	customDir(t, e)
	n := &fakeNotifier{}
	e.m.runDaily(e.ctx, n)
	if got := strings.Join(e.agent.calls, " "); got != "backup.store backup.prune backup.appdata backup.prune" {
		t.Fatalf("%s", got)
	}
	if len(n.sent) != 0 {
		t.Fatalf("%v", n.sent)
	}
	// Fehler der appdata-Sicherung wird gemeldet, die Konfiguration bleibt trotzdem gesichert
	e.agent.calls = nil
	e.agent.onCall = func(method string, _ any) error {
		if method == "backup.appdata" {
			return errors.New("Platte voll")
		}
		return nil
	}
	e.m.runDaily(e.ctx, n)
	if len(n.sent) != 1 || !strings.Contains(n.sent[0].Body, "appdata") || !strings.Contains(n.sent[0].Body, "Platte voll") {
		t.Fatalf("%+v", n.sent)
	}
}
