package docker

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]uint64{"0B": 0, "10.5MiB": 11010048, "1GiB": 1 << 30, "2kB": 2000, "1.5GB": 1500000000, "7.7GiB": 8267812044, "": 0, "x": 0}
	for in, want := range cases {
		if got := ParseSize(in); got != want {
			t.Errorf("%q: %d, erwartet %d", in, got, want)
		}
	}
}

func TestParseStats(t *testing.T) {
	out := []byte(`{"ID":"aaaaaaaaaaaabbbbbbbb","Name":"web","CPUPerc":"1.50%","MemUsage":"10MiB / 1GiB","MemPerc":"0.98%"}
{"ID":"cccccccccccc","Name":"api","CPUPerc":"--","MemUsage":"0B / 0B","MemPerc":"--"}
`)
	got, err := ParseStats(out)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].Name != "api" || got[0].CPU != 0 {
		t.Errorf("%+v", got[0])
	}
	w := got[1]
	if w.ID != "aaaaaaaaaaaa" || w.CPU != 1.5 || w.MemUsage != 10<<20 || w.MemLimit != 1<<30 || w.MemPercent != 0.98 {
		t.Errorf("%+v", w)
	}
}
