package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// oneLine ersetzt Zeilenumbrüche durch Leerzeichen (Schutz vor Header-Injektion).
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

func buildMail(c Mail, m Message) ([]byte, string, []string, error) {
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return nil, "", nil, err
	}
	var to []string
	var rcpt []string
	for _, a := range c.To {
		p, err := mail.ParseAddress(a)
		if err != nil {
			return nil, "", nil, err
		}
		to = append(to, p.String())
		rcpt = append(rcpt, p.Address)
	}
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from.String())
	h("To", strings.Join(to, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", "[BetterNAS] "+oneLine(m.Title)))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(m.Body))
	_ = w.Close()
	return b.Bytes(), from.Address, rcpt, nil
}

func (s *Service) sendMail(ctx context.Context, c Mail, m Message) error {
	msg, from, rcpt, err := buildMail(c, m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	conn, err := s.dial(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	dl, _ := ctx.Deadline()
	_ = conn.SetDeadline(dl)
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.Security == "tls" {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = tc
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer cl.Close()
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return fmt.Errorf("der Server bietet kein STARTTLS an")
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpt {
		if err := cl.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}
