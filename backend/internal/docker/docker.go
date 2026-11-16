// Package docker enthält Namen, Pfade und Parser für die Docker-Verwaltung (Compose-Stacks, Container).
package docker

import "regexp"

const (
	// StacksDir enthält je Stack ein Verzeichnis mit compose.yaml und .env.
	StacksDir   = "/srv/appdata/stacks"
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
var StackActions = map[string]bool{"up": true, "down": true, "stop": true, "start": true, "restart": true, "pull": true, "update": true}

// ContainerActions sind die erlaubten Aktionen auf einem Container.
var ContainerActions = map[string]bool{"start": true, "stop": true, "restart": true, "remove": true}

// StackDir ist das Verzeichnis eines Stacks. Der Name muss vorher mit StackNameRe geprüft sein.
func StackDir(name string) string { return StacksDir + "/" + name }

// Container ist ein Docker-Container in der Liste.
type Container struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Stack   string   `json:"stack"`
	Ports   []string `json:"ports"`
	Created string   `json:"created"`
}

// StackInfo ist ein Stack in der Liste.
type StackInfo struct {
	Name    string `json:"name"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
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
