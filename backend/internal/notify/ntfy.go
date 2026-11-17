package notify

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

var ntfyPriority = map[Level]string{"info": "3", "warn": "4", "crit": "5"}

func (s *Service) sendNtfy(ctx context.Context, c Ntfy, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Server, "/")+"/"+c.Topic, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", mime.QEncoding.Encode("utf-8", oneLine(m.Title)))
	prio := ntfyPriority[m.Level]
	if prio == "" {
		prio = "3"
	}
	req.Header.Set("Priority", prio)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := s.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ntfy antwortete mit HTTP %d", resp.StatusCode)
	}
	return nil
}
