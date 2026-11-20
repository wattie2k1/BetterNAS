package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

func dockerErr(err error) error {
	return apierr.New(apierr.Internal, "Docker ist nicht verfügbar").With("detail", err.Error())
}

// ListImages liefert alle Images.
func ListImages(ctx context.Context, d Deps) ([]docker.ImageInfo, error) {
	out, err := run(ctx, d.Runner, nil, "docker", "image", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerErr(err)
	}
	list, err := docker.ParseImages(out)
	if err != nil {
		return nil, err
	}
	used := usedImageIDs(ctx, d)
	if used == nil {
		return list, nil
	}
	for i := range list {
		list[i].Unused = !imageUsed(used, list[i].ID)
	}
	return list, nil
}

// usedImageIDs liefert die IDs aller Images, die ein Container (auch gestoppt) verwendet; nil, wenn das nicht ermittelbar ist.
func usedImageIDs(ctx context.Context, d Deps) []string {
	res, err := d.Runner.Run(ctx, nil, "docker", "container", "ls", "-aq", "--no-trunc")
	if err != nil || res.ExitCode != 0 {
		return nil
	}
	ids := strings.Fields(string(res.Stdout))
	if len(ids) == 0 {
		return []string{}
	}
	args := append([]string{"inspect", "--format", "{{.Image}}"}, ids...)
	res, err = d.Runner.Run(ctx, nil, "docker", args...)
	if err != nil || res.ExitCode != 0 {
		return nil
	}
	used := []string{}
	for _, id := range strings.Fields(string(res.Stdout)) {
		used = append(used, strings.TrimPrefix(id, "sha256:"))
	}
	return used
}

// imageUsed prüft, ob die (ggf. gekürzte) Image-ID zu einer verwendeten ID gehört.
func imageUsed(used []string, id string) bool {
	if id == "" {
		return false
	}
	for _, u := range used {
		if strings.HasPrefix(u, id) || strings.HasPrefix(id, u) {
			return true
		}
	}
	return false
}

// ListNetworks liefert alle Netzwerke.
func ListNetworks(ctx context.Context, d Deps) ([]docker.NetworkInfo, error) {
	out, err := run(ctx, d.Runner, nil, "docker", "network", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerErr(err)
	}
	list, err := docker.ParseNetworks(out)
	if err != nil {
		return nil, err
	}
	res, rerr := d.Runner.Run(ctx, nil, "docker", "ps", "-a", "--format", "{{.Networks}}")
	if rerr != nil || res.ExitCode != 0 {
		return list, nil
	}
	used := map[string]bool{}
	for _, l := range strings.Fields(string(res.Stdout)) {
		for _, n := range strings.Split(l, ",") {
			used[n] = true
		}
	}
	for i := range list {
		list[i].Unused = !list[i].Builtin && !used[list[i].Name]
	}
	return list, nil
}

// ListVolumes liefert alle Volumes und markiert ungenutzte.
func ListVolumes(ctx context.Context, d Deps) ([]docker.VolumeInfo, error) {
	out, err := run(ctx, d.Runner, nil, "docker", "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerErr(err)
	}
	unused := map[string]bool{}
	if res, err := d.Runner.Run(ctx, nil, "docker", "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"); err == nil && res.ExitCode == 0 {
		for _, n := range strings.Fields(string(res.Stdout)) {
			unused[n] = true
		}
	}
	return docker.ParseVolumes(out, unused)
}

// RemoveResource entfernt ein Image, Netzwerk oder Volume. ref wird je Art streng geprüft.
func RemoveResource(ctx context.Context, d Deps, kind, ref string) error {
	var args []string
	switch kind {
	case docker.KindImage:
		if !docker.ImageRefRe.MatchString(ref) {
			return apierr.New(apierr.InvalidParams, "Ungültige Image-Referenz")
		}
		args = []string{"image", "rm", ref}
	case docker.KindNetwork:
		if !docker.NetworkIDRe.MatchString(ref) {
			return apierr.New(apierr.InvalidParams, "Ungültige Netzwerk-ID")
		}
		if nets, err := ListNetworks(ctx, d); err == nil {
			for _, n := range nets {
				if n.Builtin && strings.HasPrefix(n.ID, ref[:min(len(ref), 12)]) {
					return apierr.New(apierr.Conflict, "Eingebaute Netzwerke können nicht entfernt werden.")
				}
			}
		}
		args = []string{"network", "rm", ref}
	case docker.KindVolume:
		if !docker.VolumeNameRe.MatchString(ref) {
			return apierr.New(apierr.InvalidParams, "Ungültiger Volume-Name")
		}
		args = []string{"volume", "rm", ref}
	default:
		return apierr.New(apierr.InvalidParams, "Unbekannte Ressourcenart")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", args...)
	if err != nil {
		return dockerErr(err)
	}
	if res.ExitCode != 0 {
		return apierr.New(apierr.Conflict, "Entfernen nicht möglich: "+strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// PruneResources räumt ungenutzte Ressourcen auf. all: bei Images auch ungenutzte getaggte, bei Volumes auch benannte.
func PruneResources(ctx context.Context, d Deps, kind string, all bool) (docker.PruneResult, error) {
	var args []string
	switch kind {
	case docker.KindImage:
		args = []string{"image", "prune", "-f"}
	case docker.KindNetwork:
		args = []string{"network", "prune", "-f"}
	case docker.KindVolume:
		args = []string{"volume", "prune", "-f"}
	default:
		return docker.PruneResult{}, apierr.New(apierr.InvalidParams, "Unbekannte Ressourcenart")
	}
	if all && kind != docker.KindNetwork {
		args = append(args, "-a")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", args...)
	if err != nil {
		return docker.PruneResult{}, dockerErr(err)
	}
	if res.ExitCode != 0 {
		return docker.PruneResult{}, apierr.New(apierr.Internal, "Aufräumen fehlgeschlagen: "+strings.TrimSpace(string(res.Stderr)))
	}
	return docker.PruneResult{Reclaimed: docker.ParseReclaimed(res.Stdout)}, nil
}

func registerDockerResources(s dockerReg, d Deps) {
	s.Handle("image.list", func(ctx context.Context, _ json.RawMessage) (any, error) { return ListImages(ctx, d) })
	s.Handle("network.list", func(ctx context.Context, _ json.RawMessage) (any, error) { return ListNetworks(ctx, d) })
	s.Handle("volume.list", func(ctx context.Context, _ json.RawMessage) (any, error) { return ListVolumes(ctx, d) })
	s.Handle("resource.remove", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ Kind, Ref string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		if err := RemoveResource(ctx, d, req.Kind, req.Ref); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("resource.prune", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Kind string `json:"kind"`
			All  bool   `json:"all"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return PruneResources(ctx, d, req.Kind, req.All)
	})
}
