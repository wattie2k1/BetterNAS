package vm

import (
	"strings"
	"testing"
)

func TestSharedDirsApplyAndParse(t *testing.T) {
	list := []SharedDir{{Path: "/srv/pools/test/Medien", Tag: "medien"}, {Path: "/srv/pools/test/Backup Co", Tag: "bk", ReadOnly: true}}
	u := base()
	u.SharedDirs = &list
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<memoryBacking>", "<source type='memfd'/>", "<access mode='shared'/>", "<driver type='virtiofs'/>", "<source dir='/srv/pools/test/Medien'/>", "<target dir='bk'/>", "<readonly/>"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q fehlt:\n%s", want, out)
		}
	}
	if strings.Count(out, "<memoryBacking>") != 1 || strings.Index(out, "<memoryBacking>") > strings.Index(out, "<vcpu") {
		t.Fatal("memoryBacking genau einmal und vor vcpu erwartet")
	}
	c, _ := ParseConfig([]byte(out))
	if len(c.SharedDirs) != 2 || c.SharedDirs[0] != list[0] || c.SharedDirs[1] != list[1] {
		t.Fatalf("%+v", c.SharedDirs)
	}
	// unverändert → keine Änderung, nil → bleibt
	if same, _, _ := ApplyConfig(out, u); same != out {
		t.Fatal("unveränderte Liste darf die XML nicht ändern")
	}
	u.SharedDirs = nil
	if keep, _, _ := ApplyConfig(out, u); !strings.Contains(keep, "virtiofs") {
		t.Fatal("nil muss die Ordner behalten")
	}
	// Ein Ordner weniger; dann leer → memoryBacking entfällt wieder.
	one := list[:1]
	u.SharedDirs = &one
	out1, _, _ := ApplyConfig(out, u)
	if c, _ = ParseConfig([]byte(out1)); len(c.SharedDirs) != 1 || strings.Count(out1, "<memoryBacking>") != 1 {
		t.Fatal(out1)
	}
	none := []SharedDir{}
	u.SharedDirs = &none
	out0, _, _ := ApplyConfig(out1, u)
	if strings.Contains(out0, "memoryBacking") || strings.Contains(out0, "filesystem") {
		t.Fatalf("%s", out0)
	}
	if out0 != string(haosXML(t)) {
		t.Fatal("Zurückbauen muss den Ausgangszustand liefern")
	}
}

func TestSharedDirsKeepForeignFilesystemAndMemoryBacking(t *testing.T) {
	foreign := "    <filesystem type='mount' accessmode='mapped'>\n      <source dir='/tmp/x'/>\n      <target dir='x'/>\n    </filesystem>\n  </devices>"
	x := strings.Replace(string(haosXML(t)), "  </devices>", foreign, 1)
	list := []SharedDir{{Path: "/srv/pools/test/a", Tag: "a"}}
	u := base()
	u.SharedDirs = &list
	out, _, err := ApplyConfig(x, u)
	if err != nil || !strings.Contains(out, "<source dir='/tmp/x'/>") {
		t.Fatalf("%v\n%s", err, out)
	}
	none := []SharedDir{}
	u.SharedDirs = &none
	if out2, _, _ := ApplyConfig(out, u); !strings.Contains(out2, "<source dir='/tmp/x'/>") || strings.Contains(out2, "virtiofs") {
		t.Fatal("fremdes Filesystem muss bleiben")
	}
	// Eigene, ungeeignete Speicher-Konfiguration wird abgelehnt.
	own := strings.Replace(string(haosXML(t)), "  <vcpu", "  <memoryBacking>\n    <hugepages/>\n  </memoryBacking>\n  <vcpu", 1)
	u.SharedDirs = &list
	if _, _, err := ApplyConfig(own, u); err == nil {
		t.Fatal("Hugepages ohne shared")
	}
}

func TestSharedDirsValidate(t *testing.T) {
	good := []SharedDir{{Path: "/srv/pools/test", Tag: "root"}, {Path: "/srv/pools/test/Ordner Mit Leer", Tag: "b_2"}, {Path: "/srv/pools/p-1/Über", Tag: "c-3"}}
	u := base()
	u.SharedDirs = &good
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := [][]SharedDir{
		{{Path: "/etc", Tag: "x"}},
		{{Path: "/srv/pools", Tag: "x"}},
		{{Path: "/srv/pools/test/../../etc", Tag: "x"}},
		{{Path: "/srv/pools/test/a'b", Tag: "x"}},
		{{Path: "/srv/pools/test/a<b", Tag: "x"}},
		{{Path: "/srv/pools/test/", Tag: "x"}},
		{{Path: "/srv/pools/test/a", Tag: ""}},
		{{Path: "/srv/pools/test/a", Tag: "bad tag"}},
		{{Path: "/srv/pools/test/a", Tag: strings.Repeat("a", 37)}},
		{{Path: "/srv/pools/test/a", Tag: "t"}, {Path: "/srv/pools/test/b", Tag: "t"}},
		make([]SharedDir, 9),
	}
	for _, b := range bad {
		b := b
		u.SharedDirs = &b
		if u.Validate() == nil {
			t.Errorf("%+v sollte ungültig sein", b)
		}
	}
}
