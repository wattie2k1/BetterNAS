package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

// MaxIconBytes begrenzt die Größe eines Stack-Icons (Upload und Abruf per URL).
const MaxIconBytes = 512 << 10

// Icon ist ein gespeichertes Stack-Icon.
type Icon struct {
	ContentType string
	Data        []byte
	Version     string
}

// iconClient ruft Icons von URLs ab (austauschbar für Tests).
var iconClient = &http.Client{Timeout: 10 * time.Second}

// sniffIcon bestimmt den Bildtyp aus den Daten (nicht aus Angaben des Absenders).
func sniffIcon(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	switch ct := http.DetectContentType(data); {
	case ct == "image/png", ct == "image/jpeg", ct == "image/gif", ct == "image/webp":
		return ct, true
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	trimmed := bytes.TrimSpace(head)
	if bytes.HasPrefix(trimmed, []byte("<svg")) || (bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(head, []byte("<svg"))) {
		return "image/svg+xml", true
	}
	return "", false
}

func (m *Manager) requireStack(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	_, err := m.GetStack(ctx, name)
	return err
}

// SetIcon speichert ein Icon für den Stack. Erlaubt sind PNG, JPEG, GIF, WebP und SVG bis MaxIconBytes.
func (m *Manager) SetIcon(ctx context.Context, actor, name string, data []byte) error {
	if err := m.requireStack(ctx, name); err != nil {
		return err
	}
	if len(data) > MaxIconBytes {
		return apierr.New(apierr.InvalidParams, "Das Icon ist zu groß (höchstens 512 KiB)")
	}
	ct, ok := sniffIcon(data)
	if !ok {
		return apierr.New(apierr.InvalidParams, "Kein unterstütztes Bild (erlaubt: PNG, JPEG, GIF, WebP, SVG)")
	}
	sum := sha256.Sum256(data)
	_, err := m.db.ExecContext(ctx, `INSERT INTO stack_icons(stack, content_type, data, version) VALUES (?, ?, ?, ?)
		ON CONFLICT(stack) DO UPDATE SET content_type = excluded.content_type, data = excluded.data, version = excluded.version`,
		name, ct, data, hex.EncodeToString(sum[:6]))
	m.record(ctx, actor, "stack.icon.set", name, err)
	return err
}

// FetchIcon lädt das Bild einer http(s)-Adresse (höchstens MaxIconBytes).
func FetchIcon(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, apierr.New(apierr.InvalidParams, "Ungültige Adresse (erlaubt: http:// oder https://)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, apierr.New(apierr.InvalidParams, "Ungültige Adresse")
	}
	resp, err := iconClient.Do(req)
	if err != nil {
		return nil, apierr.New(apierr.InvalidParams, "Die Adresse ist nicht erreichbar")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, apierr.New(apierr.InvalidParams, fmt.Sprintf("Die Adresse antwortet mit Fehler %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxIconBytes+1))
	if err != nil {
		return nil, apierr.New(apierr.InvalidParams, "Das Bild konnte nicht gelesen werden")
	}
	return data, nil
}

// SniffIcon prüft ein Icon (Größe, Bildtyp) und liefert Typ und Versionskennung.
func SniffIcon(data []byte) (contentType, version string, err error) {
	if len(data) > MaxIconBytes {
		return "", "", apierr.New(apierr.InvalidParams, "Das Icon ist zu groß (höchstens 512 KiB)")
	}
	ct, ok := sniffIcon(data)
	if !ok {
		return "", "", apierr.New(apierr.InvalidParams, "Kein unterstütztes Bild (erlaubt: PNG, JPEG, GIF, WebP, SVG)")
	}
	sum := sha256.Sum256(data)
	return ct, hex.EncodeToString(sum[:6]), nil
}

// SetIconFromURL lädt das Bild einer http(s)-Adresse einmalig und speichert es wie einen Upload.
func (m *Manager) SetIconFromURL(ctx context.Context, actor, name, rawURL string) error {
	if _, err := url.Parse(strings.TrimSpace(rawURL)); err != nil {
		return apierr.New(apierr.InvalidParams, "Ungültige Adresse (erlaubt: http:// oder https://)")
	}
	if err := m.requireStack(ctx, name); err != nil {
		return err
	}
	data, err := FetchIcon(ctx, rawURL)
	if err != nil {
		return err
	}
	return m.SetIcon(ctx, actor, name, data)
}

// GetIcon liefert das Icon des Stacks (NotFound, wenn keins gesetzt ist).
func (m *Manager) GetIcon(ctx context.Context, name string) (Icon, error) {
	if err := checkName(name); err != nil {
		return Icon{}, err
	}
	var ic Icon
	err := m.db.QueryRowContext(ctx, `SELECT content_type, data, version FROM stack_icons WHERE stack = ?`, name).Scan(&ic.ContentType, &ic.Data, &ic.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Icon{}, apierr.New(apierr.NotFound, "Kein Icon gesetzt")
	}
	return ic, err
}

// DeleteIcon entfernt das Icon (ohne Fehler, wenn keins gesetzt war).
func (m *Manager) DeleteIcon(ctx context.Context, actor, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `DELETE FROM stack_icons WHERE stack = ?`, name)
	m.record(ctx, actor, "stack.icon.delete", name, err)
	return err
}

// iconVersions liefert die Icon-Version je Stack (für die Stack-Liste).
func (m *Manager) iconVersions(ctx context.Context) (map[string]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT stack, version FROM stack_icons`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var s, v string
		if err := rows.Scan(&s, &v); err != nil {
			return nil, err
		}
		out[s] = v
	}
	return out, rows.Err()
}
