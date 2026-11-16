package docker

import "testing"

func TestParseImages(t *testing.T) {
	out := []byte(`{"ID":"sha256:aaaaaaaaaaaa","Repository":"nginx","Tag":"stable","Size":"192MB","CreatedSince":"2 weeks ago"}
{"ID":"bbbbbbbbbbbb","Repository":"<none>","Tag":"<none>","Size":"1.2GB","CreatedSince":"1 month ago"}
{"ID":"cccccccccccc","Repository":"alpine","Tag":"<none>","Size":"8MB","CreatedSince":"x"}
`)
	got, err := ParseImages(out)
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	if got[0].Ref != "alpine" && got[0].Ref != "cccccccccccc" {
		t.Errorf("getaggte zuerst, ohne Tag über ID: %+v", got[0])
	}
	var nginx, dangling ImageInfo
	for _, im := range got {
		switch {
		case im.Repository == "nginx":
			nginx = im
		case im.Dangling:
			dangling = im
		}
	}
	if nginx.Ref != "nginx:stable" || nginx.ID != "aaaaaaaaaaaa" || nginx.SizeBytes != 192_000_000 {
		t.Errorf("%+v", nginx)
	}
	if !dangling.Dangling || dangling.Ref != "bbbbbbbbbbbb" || got[2].ID != "bbbbbbbbbbbb" {
		t.Errorf("%+v", dangling)
	}
}

func TestParseNetworksAndVolumes(t *testing.T) {
	nets, err := ParseNetworks([]byte(`{"ID":"111111111111","Name":"media_default","Driver":"bridge","Scope":"local"}
{"ID":"222222222222","Name":"bridge","Driver":"bridge","Scope":"local"}
`))
	if err != nil || len(nets) != 2 || nets[0].Name != "bridge" || !nets[0].Builtin || nets[1].Builtin {
		t.Fatalf("%+v %v", nets, err)
	}
	vols, err := ParseVolumes([]byte(`{"Name":"b","Driver":"local"}
{"Name":"a","Driver":"local"}
`), map[string]bool{"b": true})
	if err != nil || len(vols) != 2 || vols[0].Name != "a" || vols[0].Unused || !vols[1].Unused {
		t.Fatalf("%+v %v", vols, err)
	}
}

func TestParseReclaimed(t *testing.T) {
	if got := ParseReclaimed([]byte("Deleted Images:\nx\n\nTotal reclaimed space: 1.2GB\n")); got != "1.2GB" {
		t.Fatal(got)
	}
	if ParseReclaimed([]byte("nichts")) != "" {
		t.Fatal()
	}
}
