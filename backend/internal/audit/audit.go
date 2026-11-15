// Package audit protokolliert Änderungen am System.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Entry ist ein neuer Audit-Eintrag. Result ist "ok" oder "error".
type Entry struct {
	Actor, Action, Target, Result string
	Details                       map[string]any
}

// StoredEntry ist ein gespeicherter Audit-Eintrag.
type StoredEntry struct {
	ID      int64          `json:"id"`
	Time    time.Time      `json:"time"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Target  string         `json:"target"`
	Result  string         `json:"result"`
	Details map[string]any `json:"details,omitempty"`
}

// Record speichert einen Eintrag.
func Record(ctx context.Context, d *sql.DB, e Entry) error {
	var details []byte
	if len(e.Details) > 0 {
		var err error
		if details, err = json.Marshal(e.Details); err != nil {
			return err
		}
	}
	_, err := d.ExecContext(ctx,
		`INSERT INTO audit(ts, actor, action, target, result, details_json) VALUES (?,?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339Nano), e.Actor, e.Action, e.Target, e.Result, nullable(details))
	return err
}

func nullable(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// List liefert die neuesten Einträge zuerst.
func List(ctx context.Context, d *sql.DB, limit int) ([]StoredEntry, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, ts, actor, action, target, result, details_json FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredEntry
	for rows.Next() {
		var e StoredEntry
		var ts string
		var details sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.Target, &e.Result, &details); err != nil {
			return nil, err
		}
		e.Time, _ = time.Parse(time.RFC3339Nano, ts)
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
