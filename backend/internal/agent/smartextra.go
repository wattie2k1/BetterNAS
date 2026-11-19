package agent

import (
	"strconv"
	"strings"
)

// SmartAttr ist ein SMART-Attribut. Bei NVMe-Platten (ohne Attributtabelle) stehen hier die Gesundheitswerte mit ID 0
// und nur Name und Rohwert.
type SmartAttr struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Value     int    `json:"value"`
	Worst     int    `json:"worst"`
	Threshold int    `json:"threshold"`
	Raw       string `json:"raw"`
	Prefail   bool   `json:"prefail,omitempty"`
	// Failing: der Wert hat die Schwelle unterschritten (jetzt oder früher).
	Failing bool `json:"failing,omitempty"`
}

// Wear beschreibt den Verschleiß einer SSD.
type Wear struct {
	PercentUsed  *int   `json:"percentUsed,omitempty"`
	SpareLeft    *int   `json:"spareLeftPercent,omitempty"`
	BytesWritten uint64 `json:"bytesWritten,omitempty"`
}

type ataAttr struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Value  int    `json:"value"`
	Worst  int    `json:"worst"`
	Thresh int    `json:"thresh"`
	When   string `json:"when_failed"`
	Flags  struct {
		Prefailure bool `json:"prefailure"`
	} `json:"flags"`
	Raw struct {
		Value int    `json:"value"`
		Str   string `json:"string"`
	} `json:"raw"`
}

type nvmeHealth struct {
	CriticalWarning int    `json:"critical_warning"`
	AvailableSpare  *int   `json:"available_spare"`
	SpareThreshold  int    `json:"available_spare_threshold"`
	PercentageUsed  *int   `json:"percentage_used"`
	DataUnitsRead   uint64 `json:"data_units_read"`
	DataUnitsWrite  uint64 `json:"data_units_written"`
	PowerCycles     int    `json:"power_cycles"`
	UnsafeShutdowns int    `json:"unsafe_shutdowns"`
	MediaErrors     int    `json:"media_errors"`
	ErrorLogEntries int    `json:"num_err_log_entries"`
}

// wearLifeIDs sind ATA-Attribute, deren normierter Wert die verbleibende Lebensdauer in Prozent angibt
// (SSD_Life_Left, Media_Wearout_Indicator, Wear_Leveling_Count, Percent_Lifetime_Remain), nach Verlässlichkeit geordnet.
var wearLifeIDs = []int{231, 233, 177, 202}

func smartExtras(ata []ataAttr, nvme *nvmeHealth, blockSize int) ([]SmartAttr, *Wear) {
	if nvme != nil {
		return nvmeExtras(nvme)
	}
	if len(ata) == 0 {
		return nil, nil
	}
	attrs := make([]SmartAttr, 0, len(ata))
	byID := map[int]ataAttr{}
	for _, a := range ata {
		raw := a.Raw.Str
		if raw == "" {
			raw = smartInt(a.Raw.Value)
		}
		attrs = append(attrs, SmartAttr{ID: a.ID, Name: a.Name, Value: a.Value, Worst: a.Worst, Threshold: a.Thresh, Raw: raw, Prefail: a.Flags.Prefailure, Failing: a.When != "" && a.When != "-"})
		byID[a.ID] = a
	}
	var w Wear
	for _, id := range wearLifeIDs {
		if a, ok := byID[id]; ok && a.Value >= 1 && a.Value <= 100 {
			used := 100 - a.Value
			w.PercentUsed = &used
			break
		}
	}
	if blockSize <= 0 {
		blockSize = 512
	}
	for _, id := range []int{241, 246} {
		if a, ok := byID[id]; ok && strings.Contains(strings.ToUpper(a.Name), "LBA") && a.Raw.Value > 0 {
			w.BytesWritten = uint64(a.Raw.Value) * uint64(blockSize)
			break
		}
	}
	if w.PercentUsed == nil && w.BytesWritten == 0 {
		return attrs, nil
	}
	return attrs, &w
}

// nvmeUnit: ein „data unit“ sind 1000 Blöcke à 512 Byte.
const nvmeUnit = 512000

func nvmeExtras(h *nvmeHealth) ([]SmartAttr, *Wear) {
	attrs := []SmartAttr{
		{Name: "Kritische Warnungen", Raw: smartInt(h.CriticalWarning), Failing: h.CriticalWarning != 0},
		{Name: "Verbrauchte Lebensdauer (%)", Raw: ptrStr(h.PercentageUsed)},
		{Name: "Reserve-Speicher (%)", Raw: ptrStr(h.AvailableSpare), Threshold: h.SpareThreshold},
		{Name: "Gelesen (Bytes)", Raw: u64str(h.DataUnitsRead * nvmeUnit)},
		{Name: "Geschrieben (Bytes)", Raw: u64str(h.DataUnitsWrite * nvmeUnit)},
		{Name: "Einschaltvorgänge", Raw: smartInt(h.PowerCycles)},
		{Name: "Unsichere Abschaltungen", Raw: smartInt(h.UnsafeShutdowns)},
		{Name: "Medienfehler", Raw: smartInt(h.MediaErrors), Failing: h.MediaErrors > 0},
		{Name: "Einträge im Fehlerprotokoll", Raw: smartInt(h.ErrorLogEntries)},
	}
	w := &Wear{PercentUsed: h.PercentageUsed, SpareLeft: h.AvailableSpare, BytesWritten: h.DataUnitsWrite * nvmeUnit}
	return attrs, w
}

func smartInt(n int) string { return strconv.Itoa(n) }

func u64str(n uint64) string { return strconv.FormatUint(n, 10) }

func ptrStr(p *int) string {
	if p == nil {
		return "–"
	}
	return strconv.Itoa(*p)
}
