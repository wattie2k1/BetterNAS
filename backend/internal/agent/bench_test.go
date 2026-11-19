package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestParseDD(t *testing.T) {
	v, ok := parseDD("256+0 records in\n256+0 records out\n268435456 bytes (268 MB, 256 MiB) copied, 1.8 s, 149 MB/s\n")
	if !ok || v < 149 || v > 150 {
		t.Fatalf("%v %v", v, ok)
	}
	if v, ok := parseDD("268435456 bytes (268 MB, 256 MiB) copied, 0,5 s, 536 MB/s"); !ok || v < 536 || v > 537 {
		t.Fatalf("Komma: %v %v", v, ok)
	}
	if _, ok := parseDD("dd: failed to open"); ok {
		t.Fatal("kein Ergebnis erwartet")
	}
}

func TestBenchReadsOnly(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["dd if=/dev/sda"] = sysexec.Result{Stderr: []byte("268435456 bytes (268 MB) copied, 2 s, 134 MB/s")}
	res, err := Bench(context.Background(), d, "/dev/sda")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != benchPoints || res.Samples[0].Percent != 0 || res.Samples[benchPoints-1].Percent != 100 {
		t.Fatalf("%+v", res)
	}
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "dd ") {
			n++
			if !strings.Contains(c, "of=/dev/null") || !strings.Contains(c, "iflag=direct") {
				t.Fatalf("nur lesen erlaubt: %s", c)
			}
		}
	}
	if n != benchPoints {
		t.Fatalf("%d dd-Aufrufe", n)
	}
}

func TestBenchErrors(t *testing.T) {
	d, f, _ := storageEnv(t)
	if code(func() error { _, e := Bench(context.Background(), d, "/dev/zzz"); return e }()) != "not_found" {
		t.Error("unbekannte Platte")
	}
	f.Responses["dd if=/dev/sda"] = sysexec.Result{ExitCode: 1, Stderr: []byte("dd: error")}
	if code(func() error { _, e := Bench(context.Background(), d, "/dev/sda"); return e }()) != "conflict" {
		t.Error("Lesefehler")
	}
}
