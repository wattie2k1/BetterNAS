package storage

import (
	"regexp"
	"strconv"
	"strings"
)

// Wartungsarten eines btrfs-Pools (laufen als eigene Unit im Agent).
const (
	OpBalance = "balance"
	OpRemove  = "remove"
	OpConvert = "convert"
)

// BtrfsOp ist der Stand der Wartung (Balance, Platte entfernen, Profil wechseln).
type BtrfsOp struct {
	Op      string `json:"op"`
	Running bool   `json:"running"`
	// Result: "" (läuft oder nie gelaufen) | ok | failed
	Result string `json:"result,omitempty"`
	// Progress in Prozent (0–100) oder -1, wenn btrfs keinen Fortschritt meldet.
	Progress int    `json:"progress"`
	Message  string `json:"message,omitempty"`
}

// BtrfsReplace ist der Stand des Plattenersatzes (btrfs replace).
type BtrfsReplace struct {
	// State: never | running | finished | canceled
	State     string  `json:"state"`
	Percent   float64 `json:"percent"`
	WriteErrs uint64  `json:"writeErrs"`
	ReadErrs  uint64  `json:"readErrs"`
}

// BtrfsOpRequest startet eine Wartung.
type BtrfsOpRequest struct {
	Name     string `json:"name"`
	Op       string `json:"op"`
	DevID    int    `json:"devId,omitempty"`    // remove: Platte
	Missing  bool   `json:"missing,omitempty"`  // remove: fehlende Platte
	Usage    int    `json:"usage,omitempty"`    // balance: nur Blöcke bis zu dieser Füllung (1–100), 0 = alles
	Profile  string `json:"profile,omitempty"`  // convert: Zielprofil der Daten
	Metadata string `json:"metadata,omitempty"` // convert: Zielprofil der Metadaten
}

// BtrfsReplaceRequest ersetzt eine Platte durch eine neue (diese wird gelöscht).
type BtrfsReplaceRequest struct {
	Name   string    `json:"name"`
	FSUUID string    `json:"fsUuid"`
	DevID  int       `json:"devId"`
	Target BtrfsDisk `json:"target"`
}

// BtrfsAddRequest fügt dem Pool eine Platte hinzu (diese wird gelöscht).
type BtrfsAddRequest struct {
	Name   string    `json:"name"`
	Target BtrfsDisk `json:"target"`
}

// BtrfsMountRequest hängt einen unvollständigen Pool bis zum nächsten Neustart degradiert ein.
type BtrfsMountRequest struct {
	Name     string `json:"name"`
	FSUUID   string `json:"fsUuid"`
	Compress string `json:"compress"`
}

// BtrfsResizeRequest vergrößert eine Platte auf ihre volle Größe (nach dem Ersetzen durch eine größere).
type BtrfsResizeRequest struct {
	Name  string `json:"name"`
	DevID int    `json:"devId"`
}

var (
	replaceDoneRe    = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)% done, (\d+) write errs, (\d+) uncorr\. read errs`)
	replaceEndRe     = regexp.MustCompile(`(\d+) write errs, (\d+) uncorr\. read errs`)
	balanceLeftRe    = regexp.MustCompile(`(\d+)% left`)
	balanceRunningRe = regexp.MustCompile(`is running`)
)

// ParseBtrfsReplace liest „btrfs replace status -1 <mnt>“.
func ParseBtrfsReplace(out string) BtrfsReplace {
	t := strings.TrimSpace(out)
	r := BtrfsReplace{State: "never"}
	if t == "" || strings.Contains(t, "Never started") {
		return r
	}
	if m := replaceDoneRe.FindStringSubmatch(t); m != nil {
		r.State = "running"
		r.Percent, _ = strconv.ParseFloat(m[1], 64)
		r.WriteErrs, _ = strconv.ParseUint(m[2], 10, 64)
		r.ReadErrs, _ = strconv.ParseUint(m[3], 10, 64)
		return r
	}
	switch {
	case strings.Contains(t, "finished on"):
		r.State, r.Percent = "finished", 100
	case strings.Contains(t, "canceled on"):
		r.State = "canceled"
	default:
		return r
	}
	if m := replaceEndRe.FindStringSubmatch(t); m != nil {
		r.WriteErrs, _ = strconv.ParseUint(m[1], 10, 64)
		r.ReadErrs, _ = strconv.ParseUint(m[2], 10, 64)
	}
	return r
}

// ParseBtrfsBalanceProgress liest „btrfs balance status <mnt>“ und liefert den Fortschritt in Prozent (ok=false: keine Balance aktiv).
func ParseBtrfsBalanceProgress(out string) (percent int, ok bool) {
	if !balanceRunningRe.MatchString(out) {
		return 0, false
	}
	if m := balanceLeftRe.FindStringSubmatch(out); m != nil {
		left, _ := strconv.Atoi(m[1])
		return max(0, min(100, 100-left)), true
	}
	return 0, true
}
