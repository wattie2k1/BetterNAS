// Package docker enthält Namen, Pfade und Parser für die Docker-Verwaltung (Compose-Stacks, Container).
package docker

import "regexp"

const (
	ComposeFile = "compose.yaml"
	EnvFile     = ".env"

	MaxComposeBytes = 256 << 10
	MaxEnvBytes     = 64 << 10
)

var (
	// StackNameRe ist der erlaubte Stackname (zugleich Compose-Projektname).
	StackNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	// ContainerIDRe erlaubt nur Container-IDs (keine Namen), damit nichts als Option gedeutet wird.
	ContainerIDRe = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	// RunUnitRe erkennt die transienten Units der Stack-Läufe.
	RunUnitRe = regexp.MustCompile(`^betternas-stack-[a-z0-9_-]+-[0-9]+$`)
)

// RunUnitPrefix ist der Anfang der Unit-Namen der Stack-Läufe.
const RunUnitPrefix = "betternas-stack-"

// StackActions sind die erlaubten Aktionen auf einem Stack.
var StackActions = map[string]bool{"up": true, "down": true, "stop": true, "start": true, "restart": true, "pull": true, "update": true, "rollback": true}

// ContainerActions sind die erlaubten Aktionen auf einem Container.
var ContainerActions = map[string]bool{"start": true, "stop": true, "restart": true, "remove": true}

// Container ist ein Docker-Container in der Liste.
type Container struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	// Health ist der Healthcheck-Zustand: healthy | unhealthy | starting (leer: Container hat keinen Healthcheck).
	Health  string   `json:"health,omitempty"`
	Stack   string   `json:"stack"`
	Ports   []string `json:"ports"`
	Created string   `json:"created"`
}

// StackInfo ist ein Stack in der Liste.
type StackInfo struct {
	Name    string `json:"name"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
	// Icon ist die Version des Stack-Icons (leer: keins gesetzt); sie dient als Cache-Schlüssel für die Icon-Adresse.
	Icon string `json:"icon,omitempty"`
	// Rollback ist der Zeitpunkt (RFC 3339) des Stands, auf den ein Rollback zurückgeht (leer: kein Rollback möglich).
	Rollback string `json:"rollback,omitempty"`
	// RollbackData sagt, dass zum Rollback-Stand auch eine Datensicherung gehört (Daten lassen sich mit zurücksetzen).
	RollbackData bool `json:"rollbackData,omitempty"`
}

// RollbackImage ist ein Image des Rollback-Stands: Referenz aus der Compose-Datei und die damalige lokale ID.
type RollbackImage struct {
	Ref string `json:"ref"`
	ID  string `json:"id"`
}

// Rollback ist der Stand eines Stacks vor dem letzten Update, das Images verändert hat.
type Rollback struct {
	CreatedAt string          `json:"createdAt"`
	Images    []RollbackImage `json:"images"`
	// Backup ist die Kennung der Datensicherung, die vor dem Update gemacht wurde (leer: keine).
	Backup string `json:"backup,omitempty"`
}

// BackupSource ist ein Datenbereich eines Stacks, der vor einem Update gesichert wird.
type BackupSource struct {
	Kind  string `json:"kind"`  // bind | volume
	Name  string `json:"name"`  // Anzeige: Ordnerpfad bzw. Volume-Name
	Path  string `json:"path"`  // Pfad auf dem Host
	Bytes int64  `json:"bytes"` // Größe der Daten
}

// BackupPlan beschreibt, was vor einem Update gesichert würde und ob das möglich ist.
type BackupPlan struct {
	Sources    []BackupSource `json:"sources"`
	TotalBytes int64          `json:"totalBytes"`
	FreeBytes  int64          `json:"freeBytes"`
	Possible   bool           `json:"possible"`
	Reason     string         `json:"reason,omitempty"` // warum nicht möglich
}

// DataBackup ist die Beschreibung einer Datensicherung (liegt neben dem Archiv).
type DataBackup struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"createdAt"`
	Sources   []BackupSource `json:"sources"`
}

// RunOptions sind Zusatzwünsche zu einer Stack-Aktion.
type RunOptions struct {
	// Backup sichert vor einem Update die Daten des Stacks (nur für „update“).
	Backup bool `json:"backup,omitempty"`
	// RestoreData setzt bei einem Rollback auch die Daten auf den gesicherten Stand zurück (nur für „rollback“).
	RestoreData bool `json:"restoreData,omitempty"`
}

// RunState ist der Zustand eines Stack-Laufs.
type RunState struct {
	State string `json:"state"` // running | succeeded | failed
	Log   string `json:"log"`
}

// Status beschreibt, ob Docker verfügbar ist.
type Status struct {
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Version   string `json:"version"`
}

// StackFiles sind compose.yaml und .env eines Stacks.
type StackFiles struct {
	Name    string `json:"name"`
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// StackCheck ist das Ergebnis der Compose-Prüfung.
type StackCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ContainerStats sind die Auslastungswerte eines laufenden Containers.
type ContainerStats struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CPU        float64 `json:"cpu"`        // Prozent eines Kerns (kann > 100 sein)
	MemUsage   uint64  `json:"memUsage"`   // Bytes
	MemLimit   uint64  `json:"memLimit"`   // Bytes
	MemPercent float64 `json:"memPercent"` // Prozent
}

// ImageRefRe erlaubt Image-Referenzen (keine führenden Bindestriche, keine Leerzeichen).
var ImageRefRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)

// ImageStatus ist das Ergebnis des Digest-Vergleichs eines Images.
type ImageStatus struct {
	Image    string `json:"image"`
	Local    string `json:"local"`
	Remote   string `json:"remote"`
	Outdated bool   `json:"outdated"`
	Error    string `json:"error,omitempty"`
}

// UpdateInfo beschreibt den Update-Stand eines Stacks.
type UpdateInfo struct {
	Outdated []string `json:"outdated"`
	Errors   []string `json:"errors"`
}

// UpdateReport ist das Ergebnis der letzten Update-Prüfung.
type UpdateReport struct {
	CheckedAt string                `json:"checkedAt"`
	Stacks    map[string]UpdateInfo `json:"stacks"`
}
