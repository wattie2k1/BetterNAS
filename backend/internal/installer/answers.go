// Package installer enthält die Logik des BetterNAS-Installers.
package installer

// NetConfig beschreibt die Netzwerkkonfiguration des Zielsystems.
type NetConfig struct {
	Interface string   `json:"interface"`
	DHCP      bool     `json:"dhcp"`
	Address   string   `json:"address,omitempty"` // CIDR, z. B. "192.168.1.20/24"
	Gateway   string   `json:"gateway,omitempty"`
	DNS       []string `json:"dns,omitempty"`
}

// Answers sind alle Eingaben für eine Installation.
type Answers struct {
	Language      string    `json:"language"`
	Keymap        string    `json:"keymap"`
	Timezone      string    `json:"timezone"`
	TargetDevice  string    `json:"targetDevice"`
	RootSizeGiB   int       `json:"rootSizeGiB"`
	CreateAppdata bool      `json:"createAppdata"`
	Hostname      string    `json:"hostname"`
	Net           NetConfig `json:"net"`
	AdminUser     string    `json:"adminUser"`
	AdminPassword string    `json:"adminPassword"`
	ConfirmWipe   string    `json:"confirmWipe,omitempty"`
	// AdminSSHKey ist ein optionaler öffentlicher SSH-Schlüssel für den Admin (nur unbeaufsichtigt).
	AdminSSHKey string `json:"adminSSHKey,omitempty"`
}

// Languages sind die unterstützten Sprachen.
var Languages = []string{"de", "en"}

// Keymaps sind die unterstützten Tastaturlayouts.
var Keymaps = []string{"de", "de-nodeadkeys", "ch", "at", "us"}

// Defaults liefert die Standardantworten.
func Defaults() Answers {
	return Answers{
		Language:      "de",
		Keymap:        "de",
		Timezone:      "Europe/Berlin",
		RootSizeGiB:   32,
		CreateAppdata: true,
		Hostname:      "betternas",
		Net:           NetConfig{DHCP: true},
	}
}

// Locale liefert die Locale für die gewählte Sprache.
func (a Answers) Locale() string {
	if a.Language == "en" {
		return "en_US.UTF-8"
	}
	return "de_DE.UTF-8"
}

// XKB liefert Layout und Variante für /etc/default/keyboard.
func (a Answers) XKB() (layout, variant string) {
	switch a.Keymap {
	case "de-nodeadkeys":
		return "de", "nodeadkeys"
	case "at":
		return "at", ""
	case "ch":
		return "ch", ""
	case "us":
		return "us", ""
	}
	return "de", ""
}

// ConsoleKeymap liefert den Namen für loadkeys im Live-System.
func ConsoleKeymap(keymap string) string {
	switch keymap {
	case "de-nodeadkeys":
		return "de-latin1-nodeadkeys"
	case "ch":
		return "sg"
	case "at":
		return "de"
	}
	return keymap
}
