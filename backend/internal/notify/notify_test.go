package notify

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/events"
)

// fakeSMTP ist ein minimaler SMTP-Server; jede empfangene Mail landet roh im Kanal.
func fakeSMTP(t *testing.T, hang bool) (port int, msgs chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	msgs = make(chan string, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if hang {
					time.Sleep(5 * time.Second)
					return
				}
				r := bufio.NewReader(c)
				io.WriteString(c, "220 fake\r\n")
				var rcpt []string
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						io.WriteString(c, "250 fake\r\n")
					case strings.HasPrefix(cmd, "RCPT"):
						rcpt = append(rcpt, strings.TrimSpace(line))
						io.WriteString(c, "250 ok\r\n")
					case strings.HasPrefix(cmd, "MAIL"):
						io.WriteString(c, "250 ok\r\n")
					case cmd == "DATA":
						io.WriteString(c, "354 go\r\n")
						var b strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							b.WriteString(l)
						}
						msgs <- strings.Join(rcpt, "\n") + "\n\n" + b.String()
						io.WriteString(c, "250 queued\r\n")
					case cmd == "QUIT":
						io.WriteString(c, "221 bye\r\n")
						return
					default:
						io.WriteString(c, "250 ok\r\n")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, msgs
}

func mailConfig(port int) Config {
	c := defaults()
	c.Mail = Mail{Enabled: true, Host: "127.0.0.1", Port: port, Security: "none", From: "nas@example.org", To: []string{"a@example.org", "b@example.org"}}
	return c
}

func TestMailSendsViaFakeSMTP(t *testing.T) {
	d := testDB(t)
	port, msgs := fakeSMTP(t, false)
	if err := SaveConfig(context.Background(), d, mailConfig(port)); err != nil {
		t.Fatal(err)
	}
	s := NewService(d, nil)
	res := s.Send(context.Background(), Message{Level: "warn", Title: "Platte zu heiß", Body: "Temperatur 60 °C – bitte prüfen"})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("%+v", res)
	}
	raw := <-msgs
	if !strings.Contains(raw, "a@example.org") || !strings.Contains(raw, "b@example.org") {
		t.Fatalf("Empfänger fehlen: %s", raw)
	}
	_, body, _ := strings.Cut(raw, "\n\n")
	m, err := mail.ReadMessage(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if subj != "[BetterNAS] Platte zu heiß" {
		t.Fatalf("Betreff: %q", subj)
	}
	text, _ := io.ReadAll(quotedprintable.NewReader(m.Body))
	if !strings.Contains(string(text), "Temperatur 60 °C – bitte prüfen") {
		t.Fatalf("Text: %q", text)
	}
}

func TestMailHeaderInjectionStripped(t *testing.T) {
	d := testDB(t)
	port, msgs := fakeSMTP(t, false)
	_ = SaveConfig(context.Background(), d, mailConfig(port))
	s := NewService(d, nil)
	s.Send(context.Background(), Message{Level: "info", Title: "A\r\nBcc: x@evil.example", Body: "b"})
	_, body, _ := strings.Cut(<-msgs, "\n\n")
	m, err := mail.ReadMessage(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.Get("Bcc") != "" {
		t.Fatalf("Header eingeschleust: %v", m.Header)
	}
}

func TestNtfyPosts(t *testing.T) {
	type got struct {
		method, path, title, prio, auth, body string
	}
	ch := make(chan got, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ch <- got{r.Method, r.URL.Path, r.Header.Get("Title"), r.Header.Get("Priority"), r.Header.Get("Authorization"), string(b)}
	}))
	defer ts.Close()
	d := testDB(t)
	c := defaults()
	c.Ntfy = Ntfy{Enabled: true, Server: ts.URL, Topic: "nas", Token: "tok"}
	if err := SaveConfig(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	s := NewService(d, nil)
	for lvl, want := range map[Level]string{"info": "3", "warn": "4", "crit": "5"} {
		res := s.Send(context.Background(), Message{Level: lvl, Title: "Prüfung", Body: "Text"})
		if len(res) != 1 || !res[0].OK {
			t.Fatalf("%+v", res)
		}
		g := <-ch
		if g.method != "POST" || g.path != "/nas" || g.prio != want || g.auth != "Bearer tok" || g.body != "Text" {
			t.Fatalf("%s: %+v", lvl, g)
		}
		if dec, _ := new(mime.WordDecoder).DecodeHeader(g.title); dec != "Prüfung" {
			t.Fatalf("Title %q", g.title)
		}
	}
	// ohne Token kein Authorization-Header
	c.Ntfy.Token = ""
	_ = SaveConfig(context.Background(), d, c)
	// (leeres Token behält das alte) → direkt in die DB schreiben
	_, _ = d.Exec(`UPDATE settings SET value = replace(value, '"token":"tok"', '"token":""') WHERE key = 'notify.config'`)
	s.Send(context.Background(), Message{Level: "info", Title: "x", Body: "y"})
	if g := <-ch; g.auth != "" {
		t.Fatalf("Authorization trotz fehlendem Token: %q", g.auth)
	}
}

func TestSendOneChannelFailsOtherStillSends(t *testing.T) {
	ch := make(chan struct{}, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ch <- struct{}{} }))
	defer ts.Close()
	d := testDB(t)
	c := mailConfig(1) // Port 1: geschlossen
	c.Mail.Password = "supergeheim"
	c.Ntfy = Ntfy{Enabled: true, Server: ts.URL, Topic: "nas"}
	if err := SaveConfig(context.Background(), d, c); err != nil {
		t.Fatal(err)
	}
	s := NewService(d, nil)
	res := s.Send(context.Background(), Message{Level: "crit", Title: "t", Body: "b"})
	if len(res) != 2 || res[0].Channel != "mail" || res[0].OK || res[1].Channel != "ntfy" || !res[1].OK {
		t.Fatalf("%+v", res)
	}
	if res[0].Error == "" || strings.Contains(res[0].Error, "supergeheim") {
		t.Fatalf("Fehlertext: %q", res[0].Error)
	}
	<-ch
	h, err := s.History(context.Background(), 10)
	if err != nil || len(h) != 1 || len(h[0].Results) != 2 || h[0].Title != "t" {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestSendTimeout(t *testing.T) {
	d := testDB(t)
	port, _ := fakeSMTP(t, true)
	_ = SaveConfig(context.Background(), d, mailConfig(port))
	s := NewService(d, nil)
	s.timeout = 200 * time.Millisecond
	start := time.Now()
	res := s.Send(context.Background(), Message{Level: "info", Title: "t", Body: "b"})
	if time.Since(start) > 2*time.Second || len(res) != 1 || res[0].OK {
		t.Fatalf("%v %+v", time.Since(start), res)
	}
}

func TestHistoryTrimAndPublish(t *testing.T) {
	d := testDB(t)
	hub := events.NewHub()
	evs, cancel := hub.Subscribe()
	defer cancel()
	s := NewService(d, hub)
	for i := 0; i < 205; i++ {
		s.Send(context.Background(), Message{Level: "info", Title: "n" + strconv.Itoa(i), Body: "b"})
	}
	h, err := s.History(context.Background(), 500)
	if err != nil || len(h) != 200 {
		t.Fatalf("%d %v", len(h), err)
	}
	if h[0].Title != "n204" {
		t.Fatalf("neueste zuerst erwartet: %s", h[0].Title)
	}
	select {
	case m := <-evs:
		if m.Type != "notifications.changed" {
			t.Fatal(m)
		}
	default:
		t.Fatal("kein Ereignis")
	}
}

func TestSendTestUnknownOrDisabledChannel(t *testing.T) {
	s := NewService(testDB(t), nil)
	if err := s.SendTest(context.Background(), "sms"); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if err := s.SendTest(context.Background(), "mail"); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}

func TestSendTestReportsError(t *testing.T) {
	d := testDB(t)
	_ = SaveConfig(context.Background(), d, mailConfig(1))
	s := NewService(d, nil)
	err := s.SendTest(context.Background(), "mail")
	if apierr.From(err).Code != apierr.Conflict || err == nil {
		t.Fatalf("%v", err)
	}
}
