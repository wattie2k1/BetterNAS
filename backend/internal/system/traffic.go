package system

import (
	"context"
	"database/sql"
	"time"

	"github.com/betternas/betternas/internal/agent"
)

const dayLayout = "2006-01-02"

// TrafficEntry ist die Datenmenge eines Zeitraums; Key ist "2026-11-21", "2026-11" oder "2026".
type TrafficEntry struct {
	Key string `json:"key"`
	Rx  int64  `json:"rx"`
	Tx  int64  `json:"tx"`
}

// Traffic fasst die Datenmengen nach Tag (30), Monat (12) und Jahr zusammen, jeweils aufsteigend.
type Traffic struct {
	Days   []TrafficEntry `json:"days"`
	Months []TrafficEntry `json:"months"`
	Years  []TrafficEntry `json:"years"`
}

// SetStore schaltet die dauerhafte Mengenstatistik ein: aus den Zählerdifferenzen der Messungen
// wird je Kalendertag die Summe aller Netzwerkkarten in die Datenbank geschrieben.
func (s *Sampler) SetStore(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	if s.now == nil {
		s.now = time.Now
	}
}

// account verbucht den Zuwachs der Zähler seit der letzten Messung. Zähler, die zurückgesprungen sind,
// und neue Karten zählen nicht (sonst gäbe es Ausreißer nach Neustart oder Treiberwechsel).
func (s *Sampler) account(ctx context.Context, cur []agent.NetIface) error {
	s.mu.Lock()
	prev := s.prevNet
	s.prevNet = map[string][2]uint64{}
	var rx, tx uint64
	for _, n := range cur {
		s.prevNet[n.Name] = [2]uint64{n.RxBytes, n.TxBytes}
		if p, ok := prev[n.Name]; ok {
			if n.RxBytes >= p[0] {
				rx += n.RxBytes - p[0]
			}
			if n.TxBytes >= p[1] {
				tx += n.TxBytes - p[1]
			}
		}
	}
	db, now := s.db, s.now
	s.mu.Unlock()
	if db == nil || (rx == 0 && tx == 0) {
		return nil
	}
	_, err := db.ExecContext(ctx, `INSERT INTO net_traffic(day, rx_bytes, tx_bytes) VALUES (?,?,?)
		ON CONFLICT(day) DO UPDATE SET rx_bytes = rx_bytes + excluded.rx_bytes, tx_bytes = tx_bytes + excluded.tx_bytes`,
		now().Format(dayLayout), int64(rx), int64(tx))
	return err
}

// Traffic liest die Statistik; ohne Datenbank ist sie leer.
func (s *Sampler) Traffic(ctx context.Context) (Traffic, error) {
	out := Traffic{Days: []TrafficEntry{}, Months: []TrafficEntry{}, Years: []TrafficEntry{}}
	s.mu.Lock()
	db, now := s.db, s.now
	s.mu.Unlock()
	if db == nil {
		return out, nil
	}
	t := now()
	query := func(keyLen int, since string, limit int) ([]TrafficEntry, error) {
		rows, err := db.QueryContext(ctx, `SELECT k, rx, tx FROM (
				SELECT substr(day, 1, ?) AS k, SUM(rx_bytes) AS rx, SUM(tx_bytes) AS tx FROM net_traffic WHERE day >= ? GROUP BY k ORDER BY k DESC LIMIT ?
			) ORDER BY k`, keyLen, since, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list := []TrafficEntry{}
		for rows.Next() {
			var e TrafficEntry
			if err := rows.Scan(&e.Key, &e.Rx, &e.Tx); err != nil {
				return nil, err
			}
			list = append(list, e)
		}
		return list, rows.Err()
	}
	var err error
	if out.Days, err = query(10, t.AddDate(0, 0, -29).Format(dayLayout), 30); err != nil {
		return out, err
	}
	if out.Months, err = query(7, time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, -11, 0).Format(dayLayout), 12); err != nil {
		return out, err
	}
	out.Years, err = query(4, "0000-01-01", 20)
	return out, err
}
