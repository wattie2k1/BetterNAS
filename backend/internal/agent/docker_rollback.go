package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

// Rollback-Stand eines Stacks: Vor jedem Update merkt sich der Agent die lokalen Image-IDs und hält sie mit einem
// eigenen Tag (betternas-rollback/<stack>:p<i>) fest, damit Docker sie nicht als „unbenutzt“ aufräumt.
// Brachte das Update tatsächlich neue Images (StackRollbackCommit), wird der Stand zum Rollback-Stand (…:r<i>).
// Ein Rollback setzt die Tags der Compose-Images auf die alten IDs zurück und erstellt die Container neu.

const (
	rollbackFile    = ".rollback.json"
	rollbackPending = ".rollback.pending.json"
	rollbackRepo    = "betternas-rollback/"
)

var imageIDRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func rollbackPath(name, file string) string { return path.Join(docker.StackDir(name), file) }

func rollbackTag(name, kind string, i int) string {
	return fmt.Sprintf("%s%s:%s%d", rollbackRepo, name, kind, i)
}

func readRollback(d Deps, name, file string) (docker.Rollback, bool) {
	var rb docker.Rollback
	b, err := d.FS.ReadFile(rollbackPath(name, file))
	if err != nil || json.Unmarshal(b, &rb) != nil {
		return rb, false
	}
	// Nur Einträge übernehmen, die später gefahrlos an `docker tag` gehen dürfen.
	kept := rb.Images[:0]
	for _, im := range rb.Images {
		if imageIDRe.MatchString(im.ID) && docker.ImageRefRe.MatchString(im.Ref) {
			kept = append(kept, im)
		}
	}
	rb.Images = kept
	return rb, len(rb.Images) > 0
}

func writeRollback(d Deps, name, file string, rb docker.Rollback) error {
	b, err := json.Marshal(rb)
	if err != nil {
		return err
	}
	return d.FS.WriteFile(rollbackPath(name, file), b, 0o600)
}

func removeFile(d Deps, p string) {
	if d.FS.Exists(p) {
		_ = d.FS.Remove(p)
	}
}

// currentImageID liefert die lokale Image-ID einer Referenz (leer: Image nicht vorhanden).
func currentImageID(ctx context.Context, d Deps, ref string) string {
	res, err := d.Runner.Run(ctx, nil, "docker", "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	if id := strings.TrimSpace(string(res.Stdout)); imageIDRe.MatchString(id) {
		return id
	}
	return ""
}

func dropTags(ctx context.Context, d Deps, tags []string) {
	for _, t := range tags {
		_, _ = d.Runner.Run(ctx, nil, "docker", "rmi", t)
	}
}

func tagsOf(name, kind string, n int) []string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = rollbackTag(name, kind, i)
	}
	return tags
}

// snapshotStack hält die aktuellen Images eines Stacks vor einem Update fest (Best Effort: ein Fehler hier
// verhindert das Update nicht, es gibt dann nur keinen Rollback-Stand).
func snapshotStack(ctx context.Context, d Deps, name string) {
	removeFile(d, rollbackPath(name, rollbackPending))
	images, err := StackImages(ctx, d, name)
	if err != nil {
		return
	}
	rb := docker.Rollback{CreatedAt: time.Unix(nowUnix(), 0).UTC().Format(time.RFC3339)}
	for _, ref := range images {
		if strings.Contains(ref, "@") { // per Digest festgelegt: ändert sich nie
			continue
		}
		id := currentImageID(ctx, d, ref)
		if id == "" {
			continue
		}
		i := len(rb.Images)
		if res, err := d.Runner.Run(ctx, nil, "docker", "tag", id, rollbackTag(name, "p", i)); err != nil || res.ExitCode != 0 {
			continue
		}
		rb.Images = append(rb.Images, docker.RollbackImage{Ref: ref, ID: id})
	}
	if len(rb.Images) > 0 {
		_ = writeRollback(d, name, rollbackPending, rb)
	}
}

// StackRollbackCommit macht den vor dem Update gemerkten Stand zum Rollback-Stand, falls das Update
// mindestens ein Image tatsächlich verändert hat; sonst wird er verworfen.
func StackRollbackCommit(ctx context.Context, d Deps, name string) error {
	if err := checkStackName(name); err != nil {
		return err
	}
	pend, ok := readRollback(d, name, rollbackPending)
	if !ok {
		removeFile(d, rollbackPath(name, rollbackPending))
		return nil
	}
	changed := false
	for _, im := range pend.Images {
		if cur := currentImageID(ctx, d, im.Ref); cur != "" && cur != im.ID {
			changed = true
		}
	}
	if changed {
		old, hadOld := readRollback(d, name, rollbackFile)
		for i, im := range pend.Images {
			_, _ = d.Runner.Run(ctx, nil, "docker", "tag", im.ID, rollbackTag(name, "r", i))
		}
		if hadOld && len(old.Images) > len(pend.Images) {
			dropTags(ctx, d, tagsOf(name, "r", len(old.Images))[len(pend.Images):])
		}
		if err := writeRollback(d, name, rollbackFile, pend); err != nil {
			return err
		}
	}
	dropTags(ctx, d, tagsOf(name, "p", len(pend.Images)))
	removeFile(d, rollbackPath(name, rollbackPending))
	return nil
}

// StackRollbackClear verwirft den Rollback-Stand (nach einem erfolgreichen Rollback oder beim Löschen des Stacks).
func StackRollbackClear(ctx context.Context, d Deps, name string) error {
	if err := checkStackName(name); err != nil {
		return err
	}
	for _, f := range []string{rollbackFile, rollbackPending} {
		kind := "r"
		if f == rollbackPending {
			kind = "p"
		}
		if rb, ok := readRollback(d, name, f); ok {
			dropTags(ctx, d, tagsOf(name, kind, len(rb.Images)))
		}
		removeFile(d, rollbackPath(name, f))
	}
	return nil
}

// rollbackCommand baut die Befehlskette eines Rollbacks: alte IDs zurück auf die Image-Tags, dann Container neu erstellen.
func rollbackCommand(d Deps, name string, quote func([]string) string) (string, error) {
	rb, ok := readRollback(d, name, rollbackFile)
	if !ok {
		return "", apierr.New(apierr.NotFound, "Für diesen Stack gibt es keinen Rollback-Stand.")
	}
	var parts []string
	for _, im := range rb.Images {
		parts = append(parts, quote([]string{"docker", "tag", im.ID, im.Ref}))
	}
	parts = append(parts, quote(append([]string{"docker"}, composeArgs(name, "up", "-d", "--remove-orphans")...)))
	return strings.Join(parts, " && "), nil
}
