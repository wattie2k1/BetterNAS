package storage

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/betternas/betternas/internal/apierr"
)

// DiskMount ist eine eingehängte Datenplatte. Where = DiskPath(DiskID).
type DiskMount struct {
	DiskID string `json:"diskId"`
	FSUUID string `json:"fsUuid"`
	FSType string `json:"fsType"`
	Role   string `json:"role,omitempty"` // "data" (Standard) | "parity"
}

// IsParity meldet, ob die Platte eine SnapRAID-Paritätsplatte ist.
func (d DiskMount) IsParity() bool { return d.Role == RoleParity }

// Path liefert den Mountpoint: /srv/disks/<id> bzw. /srv/parity/<id>.
func (d DiskMount) Path() string {
	if d.IsParity() {
		return ParityPath(d.DiskID)
	}
	return DiskPath(d.DiskID)
}

// Pool-Typen.
const (
	PoolMergerfs = "mergerfs"
	PoolBtrfs    = "btrfs"
)

// btrfs-Profile (Daten und Metadaten) mit der Mindestanzahl an Platten.
var btrfsMinDisks = map[string]int{"single": 1, "raid1": 2, "raid1c3": 3, "raid10": 4}

// BtrfsProfiles sind die angebotenen Profile in der Reihenfolge der Oberfläche.
var BtrfsProfiles = []string{"single", "raid1", "raid1c3", "raid10"}

// BtrfsCompress sind die erlaubten Kompressionen ("" = aus).
var BtrfsCompress = []string{"", "zstd:1", "zstd:3"}

// BtrfsMinDisks liefert die Mindestanzahl der Platten für ein Profil (0 = unbekannt).
func BtrfsMinDisks(profile string) int { return btrfsMinDisks[profile] }

// Pool ist ein Pool aus mehreren Platten. Branches sind Disk-IDs in Reihenfolge.
// Typ mergerfs (Standard, auch bei leerem Type): Dateien verteilt auf einzelne Dateisysteme.
// Typ btrfs: ein gemeinsames Mehrplatten-Dateisystem (FSUUID) mit Profil.
type Pool struct {
	Name         string   `json:"name"`
	Type         string   `json:"type,omitempty"`
	Branches     []string `json:"branches"`
	CreatePolicy string   `json:"createPolicy"`
	MinFree      string   `json:"minFree"`
	Profile      string   `json:"profile,omitempty"`  // nur btrfs
	FSUUID       string   `json:"fsUuid,omitempty"`   // nur btrfs
	Compress     string   `json:"compress,omitempty"` // nur btrfs
}

// IsBtrfs meldet, ob der Pool ein btrfs-Pool ist.
func (p Pool) IsBtrfs() bool { return p.Type == PoolBtrfs }

// btrfsMembers liefert die Platten, die zu einem btrfs-Pool gehören (Disk-ID → Poolname).
func btrfsMembers(pools []Pool) map[string]string {
	out := map[string]string{}
	for _, p := range pools {
		if p.IsBtrfs() {
			for _, b := range p.Branches {
				out[b] = p.Name
			}
		}
	}
	return out
}

// State ist der gewünschte Zustand aller verwalteten Mounts.
type State struct {
	Disks []DiskMount `json:"disks"`
	Pools []Pool      `json:"pools"`
}

// Rollen eingehängter Datenträger.
const (
	RoleData   = "data"
	RoleParity = "parity"
)

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

// ParityPath liefert den Mountpoint einer Paritätsplatte.
func ParityPath(id string) string { return "/srv/parity/" + id }

// PoolPath liefert den Mountpoint eines Pools.
func PoolPath(name string) string { return "/srv/pools/" + name }

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// Validate prüft den Zustand vollständig (Namen, Werte, Referenzen).
func (s State) Validate() error {
	disks := map[string]*DiskMount{}
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
		if d.Role != "" && d.Role != RoleData && d.Role != RoleParity {
			return invalid("Unbekannte Rolle des Datenträgers: " + d.Role)
		}
		if disks[d.DiskID] != nil {
			return invalid("Datenträger doppelt: " + d.DiskID)
		}
		d := d
		disks[d.DiskID] = &d
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
		switch p.Type {
		case "", PoolMergerfs:
			if !slices.Contains(Policies, p.CreatePolicy) {
				return invalid("Unbekannte Verteilungsstrategie: " + p.CreatePolicy)
			}
			if !MinFreeRe.MatchString(p.MinFree) {
				return invalid("Mindestfreier Speicher: Zahl mit K, M oder G, z. B. 20G")
			}
		case PoolBtrfs:
			min := btrfsMinDisks[p.Profile]
			if min == 0 {
				return invalid("Unbekanntes btrfs-Profil: " + p.Profile)
			}
			if len(p.Branches) < min {
				return invalid(fmt.Sprintf("Das Profil %s braucht mindestens %d Datenträger", p.Profile, min))
			}
			if !uuidRe.MatchString(p.FSUUID) {
				return invalid("Ungültige Dateisystem-UUID des Pools")
			}
			if !slices.Contains(BtrfsCompress, p.Compress) {
				return invalid("Unbekannte Kompression: " + p.Compress)
			}
		default:
			return invalid("Unbekannter Pool-Typ: " + p.Type)
		}
		if len(p.Branches) == 0 {
			return invalid("Ein Pool braucht mindestens einen Datenträger")
		}
		for _, b := range p.Branches {
			if disks[b] == nil {
				return invalid("Datenträger ist nicht eingehängt: " + b)
			}
			if disks[b].IsParity() {
				return invalid("Eine Paritätsplatte kann nicht Teil eines Pools sein: " + b)
			}
			if p.IsBtrfs() && (disks[b].FSType != "btrfs" || disks[b].FSUUID != p.FSUUID) {
				return invalid("Datenträger " + b + " gehört nicht zum btrfs-Dateisystem des Pools")
			}
			if other, ok := used[b]; ok {
				return invalid("Datenträger " + b + " gehört bereits zu Pool " + other)
			}
			used[b] = p.Name
		}
	}
	return nil
}
