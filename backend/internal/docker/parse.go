package docker

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var healthRe = regexp.MustCompile(`\((healthy|unhealthy|health: starting)\)`)

// ParseHealth liest den Healthcheck-Zustand aus dem Status-Text von `docker ps` („Up 3 hours (healthy)“).
func ParseHealth(status string) string {
	m := healthRe.FindStringSubmatch(status)
	if m == nil {
		return ""
	}
	if m[1] == "health: starting" {
		return "starting"
	}
	return m[1]
}

// ParseContainers liest die Ausgabe von `docker ps -a --no-trunc --format '{{json .}}'` (je Zeile ein Objekt).
func ParseContainers(out []byte) ([]Container, error) {
	list := []Container{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r struct {
			ID, Names, Image, State, Status, Ports, Labels, CreatedAt string
		}
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		c := Container{ID: r.ID, Name: strings.SplitN(r.Names, ",", 2)[0], Image: r.Image, State: r.State,
			Status: r.Status, Health: ParseHealth(r.Status), Created: r.CreatedAt, Ports: []string{}}
		if c.ID != "" && len(c.ID) > 12 {
			c.ID = c.ID[:12]
		}
		for _, p := range strings.Split(r.Ports, ",") {
			// IPv6-Duplikate (":::8096->…") weglassen, sie sagen nichts Neues.
			if p = strings.TrimSpace(p); p != "" && !strings.HasPrefix(p, ":::") {
				c.Ports = append(c.Ports, p)
			}
		}
		for _, kv := range strings.Split(r.Labels, ",") {
			if v, ok := strings.CutPrefix(kv, "com.docker.compose.project="); ok {
				c.Stack = v
			}
		}
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

var statusPartRe = regexp.MustCompile(`(\w+)\((\d+)\)`)

// ParseComposeLs liest `docker compose ls -a --format json` und liefert je Stack Laufende/Gesamt.
func ParseComposeLs(out []byte) (map[string]StackInfo, error) {
	res := map[string]StackInfo{}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return res, nil
	}
	var rows []struct{ Name, Status string }
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		si := StackInfo{Name: r.Name}
		for _, m := range statusPartRe.FindAllStringSubmatch(r.Status, -1) {
			n, _ := strconv.Atoi(m[2])
			si.Total += n
			if m[1] == "running" {
				si.Running += n
			}
		}
		res[r.Name] = si
	}
	return res, nil
}

var sizeRe = regexp.MustCompile(`^([0-9.]+)\s*([A-Za-z]*)$`)

var sizeUnits = map[string]float64{
	"": 1, "b": 1,
	"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
}

// ParseSize liest Größen wie "10.5MiB" oder "1.2GB" (Docker nutzt beide Schreibweisen) in Bytes.
func ParseSize(s string) uint64 {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if err != nil || !ok {
		return 0
	}
	return uint64(v * unit)
}

func parsePercent(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return v
}

// ParseStats liest `docker stats --no-stream --no-trunc --format '{{json .}}'`.
func ParseStats(out []byte) ([]ContainerStats, error) {
	list := []ContainerStats{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r struct{ ID, Name, CPUPerc, MemUsage, MemPerc string }
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		st := ContainerStats{ID: r.ID, Name: r.Name, CPU: parsePercent(r.CPUPerc), MemPercent: parsePercent(r.MemPerc)}
		if len(st.ID) > 12 {
			st.ID = st.ID[:12]
		}
		if used, limit, ok := strings.Cut(r.MemUsage, "/"); ok {
			st.MemUsage, st.MemLimit = ParseSize(used), ParseSize(limit)
		}
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}
