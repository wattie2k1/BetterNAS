package vm

import (
	"context"
	"database/sql"
	"errors"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

// Icon ist ein gespeichertes VM-Icon.
type Icon struct {
	ContentType string
	Data        []byte
	Version     string
}

// SetIcon speichert ein Icon für die VM (PNG, JPEG, GIF, WebP oder SVG bis docker.MaxIconBytes).
func (m *Manager) SetIcon(ctx context.Context, actor, name string, data []byte) error {
	if err := checkName(name); err != nil {
		return err
	}
	ct, version, err := docker.SniffIcon(data)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO vm_icons(vm, content_type, data, version) VALUES (?, ?, ?, ?)
		ON CONFLICT(vm) DO UPDATE SET content_type = excluded.content_type, data = excluded.data, version = excluded.version`,
		name, ct, data, version)
	m.record(ctx, actor, "vm.icon.set", name, err)
	return err
}

// SetIconFromURL lädt das Bild einer http(s)-Adresse einmalig und speichert es wie einen Upload.
func (m *Manager) SetIconFromURL(ctx context.Context, actor, name, rawURL string) error {
	if err := checkName(name); err != nil {
		return err
	}
	data, err := docker.FetchIcon(ctx, rawURL)
	if err != nil {
		return err
	}
	return m.SetIcon(ctx, actor, name, data)
}

// GetIcon liefert das Icon der VM (NotFound, wenn keins gesetzt ist).
func (m *Manager) GetIcon(ctx context.Context, name string) (Icon, error) {
	if err := checkName(name); err != nil {
		return Icon{}, err
	}
	var ic Icon
	err := m.db.QueryRowContext(ctx, `SELECT content_type, data, version FROM vm_icons WHERE vm = ?`, name).Scan(&ic.ContentType, &ic.Data, &ic.Version)
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
	_, err := m.db.ExecContext(ctx, `DELETE FROM vm_icons WHERE vm = ?`, name)
	m.record(ctx, actor, "vm.icon.delete", name, err)
	return err
}

// iconVersions liefert die Icon-Version je VM.
func (m *Manager) iconVersions(ctx context.Context) (map[string]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT vm, version FROM vm_icons`)
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

// moveIcon hängt das Icon nach dem Umbenennen an den neuen Namen.
func (m *Manager) moveIcon(ctx context.Context, from, to string) {
	_, _ = m.db.ExecContext(ctx, `DELETE FROM vm_icons WHERE vm = ?`, to)
	_, _ = m.db.ExecContext(ctx, `UPDATE vm_icons SET vm = ? WHERE vm = ?`, to, from)
}
