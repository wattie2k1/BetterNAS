package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/events"
)

// Level ist die Dringlichkeit einer Meldung.
type Level string

// Message ist eine Meldung an die Kanäle.
type Message struct {
	Level Level
	Title string
	Body  string
}

// Result ist das Ergebnis eines Kanals.
type Result struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// HistoryEntry ist ein Eintrag im Verlauf.
type HistoryEntry struct {
	ID      int64     `json:"id"`
	TS      time.Time `json:"ts"`
	Level   Level     `json:"level"`
	Title   string    `json:"title"`
	Body    string    `json:"body"`
	Results []Result  `json:"results"`
}

const (
	historyKeep    = 200
	defaultTimeout = 15 * time.Second
)

// Service sendet Meldungen und führt den Verlauf.
type Service struct {
	db      *sql.DB
	hub     *events.Hub
	timeout time.Duration
	dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	httpc   *http.Client
}

// NewService erzeugt einen Service; hub darf nil sein.
func NewService(d *sql.DB, hub *events.Hub) *Service {
	return &Service{
		db: d, hub: hub, timeout: defaultTimeout,
		dial:  (&net.Dialer{}).DialContext,
		httpc: &http.Client{},
	}
}

func (s *Service) publish() {
	if s.hub != nil {
		s.hub.Publish(events.Message{Type: "notifications.changed"})
	}
}

func (s *Service) channel(ctx context.Context, c Config, name string, m Message) Result {
	var err error
	switch name {
	case "mail":
		err = s.sendMail(ctx, c.Mail, m)
	case "ntfy":
		err = s.sendNtfy(ctx, c.Ntfy, m)
	}
	if err != nil {
		label := map[string]string{"mail": "Mail", "ntfy": "ntfy"}[name]
		return Result{Channel: name, Error: label + ": " + err.Error()}
	}
	return Result{Channel: name, OK: true}
}

// Send sendet an alle aktivierten Kanäle und schreibt den Verlauf; Fehler eines Kanals stoppen die anderen nicht.
func (s *Service) Send(ctx context.Context, m Message) []Result {
	res := []Result{}
	c, err := LoadConfig(ctx, s.db)
	if err == nil {
		if c.Mail.Enabled {
			res = append(res, s.channel(ctx, c, "mail", m))
		}
		if c.Ntfy.Enabled {
			res = append(res, s.channel(ctx, c, "ntfy", m))
		}
	}
	rb, _ := json.Marshal(res)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO notifications(ts, level, title, body, results_json) VALUES (?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339), string(m.Level), m.Title, m.Body, string(rb)); err == nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id NOT IN (SELECT id FROM notifications ORDER BY id DESC LIMIT ?)`, historyKeep)
	}
	s.publish()
	return res
}

// SendTest schickt eine Testnachricht über genau einen (aktivierten) Kanal.
func (s *Service) SendTest(ctx context.Context, channel string) error {
	if channel != "mail" && channel != "ntfy" {
		return apierr.New(apierr.InvalidParams, "Unbekannter Kanal.")
	}
	c, err := LoadConfig(ctx, s.db)
	if err != nil {
		return err
	}
	if (channel == "mail" && !c.Mail.Enabled) || (channel == "ntfy" && !c.Ntfy.Enabled) {
		return apierr.New(apierr.Conflict, "Der Kanal ist nicht aktiviert (oder nicht gespeichert).")
	}
	r := s.channel(ctx, c, channel, Message{Level: "info", Title: "Testnachricht", Body: "Wenn du das liest, funktioniert dieser Kanal."})
	if !r.OK {
		return apierr.New(apierr.Conflict, r.Error)
	}
	return nil
}

// History liefert die letzten Meldungen, neueste zuerst (limit 1..200).
func (s *Service) History(ctx context.Context, limit int) ([]HistoryEntry, error) {
	limit = max(1, min(limit, historyKeep))
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, level, title, body, results_json FROM notifications ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var ts, lvl, rj string
		if err := rows.Scan(&e.ID, &ts, &lvl, &e.Title, &e.Body, &rj); err != nil {
			return nil, err
		}
		e.TS, _ = time.Parse(time.RFC3339, ts)
		e.Level = Level(lvl)
		_ = json.Unmarshal([]byte(rj), &e.Results)
		if e.Results == nil {
			e.Results = []Result{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
