package docker

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Arten von Docker-Ressourcen, die die Oberfläche verwaltet.
const (
	KindImage   = "image"
	KindNetwork = "network"
	KindVolume  = "volume"
)

var (
	// NetworkIDRe: Netzwerke werden über die ID entfernt.
	NetworkIDRe = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	// VolumeNameRe: erlaubte Volume-Namen.
	VolumeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

// BuiltinNetworks sind von Docker vorgegeben und nicht entfernbar.
var BuiltinNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

// ImageInfo ist ein Image in der Liste.
type ImageInfo struct {
	ID         string `json:"id"`
	Ref        string `json:"ref"` // repo:tag oder – ohne Tag – die ID; damit wird entfernt
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	SizeBytes  uint64 `json:"sizeBytes"`
	Created    string `json:"created"`
	Dangling   bool   `json:"dangling"`
}

// NetworkInfo ist ein Netzwerk in der Liste.
type NetworkInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Scope   string `json:"scope"`
	Builtin bool   `json:"builtin"`
}

// VolumeInfo ist ein Volume in der Liste. Unused = kein Container verwendet es.
type VolumeInfo struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Unused bool   `json:"unused"`
}

// PruneResult ist das Ergebnis von Aufräumen.
type PruneResult struct {
	Reclaimed string `json:"reclaimed"`
}

func lines(out []byte) [][]byte {
	var res [][]byte
	for _, l := range bytes.Split(out, []byte("\n")) {
		if l = bytes.TrimSpace(l); len(l) > 0 {
			res = append(res, l)
		}
	}
	return res
}

// ParseImages liest `docker image ls --format '{{json .}}'`.
func ParseImages(out []byte) ([]ImageInfo, error) {
	list := []ImageInfo{}
	for _, l := range lines(out) {
		var r struct{ ID, Repository, Tag, Size, CreatedSince string }
		if err := json.Unmarshal(l, &r); err != nil {
			return nil, err
		}
		im := ImageInfo{ID: strings.TrimPrefix(r.ID, "sha256:"), Repository: r.Repository, Tag: r.Tag, SizeBytes: ParseSize(r.Size), Created: r.CreatedSince}
		im.Dangling = r.Repository == "<none>" || r.Repository == ""
		if im.Dangling || r.Tag == "<none>" || r.Tag == "" {
			im.Ref = im.ID
			if !im.Dangling {
				im.Tag = ""
			}
		} else {
			im.Ref = r.Repository + ":" + r.Tag
		}
		if im.Dangling {
			im.Repository, im.Tag = "", ""
		}
		list = append(list, im)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Dangling != list[j].Dangling {
			return !list[i].Dangling
		}
		return list[i].Ref < list[j].Ref
	})
	return list, nil
}

// ParseNetworks liest `docker network ls --format '{{json .}}'`.
func ParseNetworks(out []byte) ([]NetworkInfo, error) {
	list := []NetworkInfo{}
	for _, l := range lines(out) {
		var r struct{ ID, Name, Driver, Scope string }
		if err := json.Unmarshal(l, &r); err != nil {
			return nil, err
		}
		list = append(list, NetworkInfo{ID: r.ID, Name: r.Name, Driver: r.Driver, Scope: r.Scope, Builtin: BuiltinNetworks[r.Name]})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Builtin != list[j].Builtin {
			return list[i].Builtin
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// ParseVolumes liest `docker volume ls --format '{{json .}}'`; unused ist die Menge ungenutzter Volume-Namen.
func ParseVolumes(out []byte, unused map[string]bool) ([]VolumeInfo, error) {
	list := []VolumeInfo{}
	for _, l := range lines(out) {
		var r struct{ Name, Driver string }
		if err := json.Unmarshal(l, &r); err != nil {
			return nil, err
		}
		list = append(list, VolumeInfo{Name: r.Name, Driver: r.Driver, Unused: unused[r.Name]})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// ParseReclaimed liest „Total reclaimed space: 1.2GB“ aus der Ausgabe eines Prune-Befehls.
func ParseReclaimed(out []byte) string {
	for _, l := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Total reclaimed space:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
