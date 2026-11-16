package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

const imageCheckTimeout = 45 * time.Second

// StackImages liefert die Images eines Stacks (`docker compose config --images`).
func StackImages(ctx context.Context, d Deps, name string) ([]string, error) {
	if err := checkStackName(name); err != nil {
		return nil, err
	}
	if !d.FS.Exists(composePath(name)) {
		return nil, apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", composeArgs(name, "config", "--images")...)
	if err != nil {
		return nil, apierr.New(apierr.Internal, "Docker ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		return nil, apierr.New(apierr.InvalidParams, "Die Compose-Datei ist ungültig: "+strings.TrimSpace(string(res.Stderr)))
	}
	seen := map[string]bool{}
	images := []string{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if ref := strings.TrimSpace(line); docker.ImageRefRe.MatchString(ref) && !seen[ref] {
			seen[ref] = true
			images = append(images, ref)
		}
	}
	sort.Strings(images)
	return images, nil
}

func digestOf(ref string) string {
	if _, d, ok := strings.Cut(ref, "@"); ok {
		return d
	}
	return ""
}

func checkImage(ctx context.Context, d Deps, ref string) docker.ImageStatus {
	st := docker.ImageStatus{Image: ref}
	ctx, cancel := context.WithTimeout(ctx, imageCheckTimeout)
	defer cancel()

	res, err := d.Runner.Run(ctx, nil, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", ref)
	if err != nil || res.ExitCode != 0 {
		st.Error = "Das Image ist lokal nicht vorhanden."
		return st
	}
	var repoDigests []string
	_ = json.Unmarshal([]byte(strings.TrimSpace(string(res.Stdout))), &repoDigests)
	local := map[string]bool{}
	for _, rd := range repoDigests {
		if dg := digestOf(rd); dg != "" {
			local[dg] = true
			if st.Local == "" {
				st.Local = dg
			}
		}
	}
	if len(local) == 0 {
		st.Error = "Das Image hat keinen Registry-Digest (lokal gebaut?)."
		return st
	}

	res, err = d.Runner.Run(ctx, nil, "docker", "buildx", "imagetools", "inspect", ref, "--format", "{{json .Manifest}}")
	// buildx 0.13 ignoriert „{{.Manifest.Digest}}“ und gibt die Textausgabe zurück; das JSON-Objekt ist verlässlich.
	var manifest struct {
		Digest string `json:"digest"`
	}
	if err == nil && res.ExitCode == 0 {
		_ = json.Unmarshal(res.Stdout, &manifest)
	}
	remote := manifest.Digest
	if err != nil || res.ExitCode != 0 || !strings.HasPrefix(remote, "sha256:") {
		st.Error = "Die Registry ist nicht erreichbar oder kennt das Image nicht."
		return st
	}
	st.Remote = remote
	st.Outdated = !local[remote]
	return st
}

// ImageDigests vergleicht lokale und Registry-Digests. Fehler eines Images betreffen nur dieses Image.
func ImageDigests(ctx context.Context, d Deps, images []string) ([]docker.ImageStatus, error) {
	if len(images) > 100 {
		return nil, apierr.New(apierr.InvalidParams, "Zu viele Images")
	}
	for _, ref := range images {
		if !docker.ImageRefRe.MatchString(ref) {
			return nil, apierr.New(apierr.InvalidParams, "Ungültige Image-Referenz")
		}
	}
	out := make([]docker.ImageStatus, 0, len(images))
	for _, ref := range images {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		out = append(out, checkImage(ctx, d, ref))
	}
	return out, nil
}

func registerDockerUpdates(s *rpc.Server, d Deps) {
	s.Handle("stack.images", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ Name string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return StackImages(ctx, d, req.Name)
	})
	s.Handle("image.digests", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Images []string `json:"images"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return ImageDigests(ctx, d, req.Images)
	})
}
