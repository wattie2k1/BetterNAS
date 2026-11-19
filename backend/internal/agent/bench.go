package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

const (
	benchPoints = 5
	benchMiB    = 256
)

// BenchSample ist eine Lesemessung an einer Stelle der Platte.
type BenchSample struct {
	Percent int     `json:"percent"` // Lage auf der Platte: 0 = Anfang, 100 = Ende
	MBps    float64 `json:"mbps"`
}

// BenchResult ist das Ergebnis des Lese-Benchmarks.
type BenchResult struct {
	Samples []BenchSample `json:"samples"`
}

var benchBusy sync.Mutex

var ddSummary = regexp.MustCompile(`(\d+) bytes .*copied, ([0-9]+(?:[.,][0-9]+)?) s`)

// parseDD liest Bytes und Sekunden aus der Abschlusszeile von dd (stderr) und liefert MB/s (Dezimal-MB).
func parseDD(stderr string) (float64, bool) {
	m := ddSummary.FindStringSubmatch(stderr)
	if m == nil {
		return 0, false
	}
	b, _ := strconv.ParseFloat(m[1], 64)
	s, err := strconv.ParseFloat(strings.Replace(m[2], ",", ".", 1), 64)
	if err != nil || s <= 0 {
		return 0, false
	}
	return b / 1e6 / s, true
}

// Bench misst die sequenzielle Leserate an fünf Stellen der Platte (nur lesend, ohne Zwischenspeicher des Kernels).
func Bench(ctx context.Context, d Deps, device string) (BenchResult, error) {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return BenchResult{}, err
	}
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device })
	if !ok {
		return BenchResult{}, apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	if !benchBusy.TryLock() {
		return BenchResult{}, apierr.New(apierr.Conflict, "Es läuft bereits ein Benchmark.")
	}
	defer benchBusy.Unlock()
	totalMiB := disk.SizeBytes >> 20
	if totalMiB < benchMiB*benchPoints {
		return BenchResult{}, apierr.New(apierr.InvalidParams, "Die Platte ist für den Benchmark zu klein.")
	}
	span := totalMiB - benchMiB
	var out BenchResult
	for i := 0; i < benchPoints; i++ {
		pct := i * 100 / (benchPoints - 1)
		skip := span * uint64(i) / (benchPoints - 1)
		res, err := d.Runner.Run(ctx, nil, "dd", "if="+device, "of=/dev/null", "bs=1M", "count="+strconv.Itoa(benchMiB), "iflag=direct", "skip="+strconv.FormatUint(skip, 10))
		if err != nil {
			return BenchResult{}, err
		}
		mbps, ok := parseDD(string(res.Stderr))
		if res.ExitCode != 0 || !ok {
			return BenchResult{}, apierr.New(apierr.Conflict, "Die Platte lässt sich nicht lesen.")
		}
		out.Samples = append(out.Samples, BenchSample{Percent: pct, MBps: mbps})
	}
	return out, nil
}

func registerBench(s *rpc.Server, d Deps) {
	s.Handle("disk.bench", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return Bench(ctx, d, req.Device)
	})
}
