package docker

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

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
			Status: r.Status, Created: r.CreatedAt, Ports: []string{}}
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
