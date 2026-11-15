package storage

import (
	"regexp"
	"slices"

	"github.com/betternas/betternas/internal/apierr"
)

// DiskMount ist eine eingehängte Datenplatte. Where = DiskPath(DiskID).
type DiskMount struct {
	DiskID string `json:"diskId"`
	FSUUID string `json:"fsUuid"`
	FSType string `json:"fsType"`
}

// Pool ist ein mergerfs-Pool. Branches sind Disk-IDs in Reihenfolge.
type Pool struct {
	Name         string   `json:"name"`
	Branches     []string `json:"branches"`
	CreatePolicy string   `json:"createPolicy"`
	MinFree      string   `json:"minFree"`
}

// State ist der gewünschte Zustand aller verwalteten Mounts.
type State struct {
	Disks []DiskMount `json:"disks"`
	Pools []Pool      `json:"pools"`
}

// Standardwerte und erlaubte Werte.
const (
	DefaultPolicy  = "epmfs"
	DefaultMinFree = "20G"
)

var (
	// Policies sind die erlaubten mergerfs-Create-Policies.
	Policies   = []string{"epmfs", "mfs", "lfs", "pfrd"}
	PoolNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	MinFreeRe  = regexp.MustCompile(`^[0-9]{1,6}[KMG]$`)
	diskIDRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	uuidRe     = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	fsTypes    = []string{"ext4", "xfs", "btrfs"}
)

// DiskPath liefert den Mountpoint einer Datenplatte.
func DiskPath(id string) string { return "/srv/disks/" + id }

// PoolPath liefert den Mountpoint eines Pools.
func PoolPath(name string) string { return "/srv/pools/" + name }

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// Validate prüft den Zustand vollständig (Namen, Werte, Referenzen).
func (s State) Validate() error {
	disks := map[string]bool{}
	for _, d := range s.Disks {
		if !diskIDRe.MatchString(d.DiskID) {
			return invalid("Ungültige Datenträger-Kennung: " + d.DiskID)
		}
		if !uuidRe.MatchString(d.FSUUID) {
			return invalid("Ungültige Dateisystem-UUID")
		}
		if !slices.Contains(fsTypes, d.FSType) {
			return invalid("Nicht unterstütztes Dateisystem: " + d.FSType)
		}
		disks[d.DiskID] = true
	}
	used := map[string]string{}
	names := map[string]bool{}
	for _, p := range s.Pools {
		if !PoolNameRe.MatchString(p.Name) {
			return invalid("Poolname: nur Kleinbuchstaben, Ziffern und Bindestrich, beginnend mit Buchstabe, max. 32 Zeichen")
		}
		if names[p.Name] {
			return invalid("Poolname bereits vergeben: " + p.Name)
		}
		names[p.Name] = true
		if !slices.Contains(Policies, p.CreatePolicy) {
			return invalid("Unbekannte Verteilungsstrategie: " + p.CreatePolicy)
		}
		if !MinFreeRe.MatchString(p.MinFree) {
			return invalid("Mindestfreier Speicher: Zahl mit K, M oder G, z. B. 20G")
		}
		if len(p.Branches) == 0 {
			return invalid("Ein Pool braucht mindestens einen Datenträger")
		}
		for _, b := range p.Branches {
			if !disks[b] {
				return invalid("Datenträger ist nicht eingehängt: " + b)
			}
			if other, ok := used[b]; ok {
				return invalid("Datenträger " + b + " gehört bereits zu Pool " + other)
			}
			used[b] = p.Name
		}
	}
	return nil
}
