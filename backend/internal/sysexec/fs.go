package sysexec

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FS abstrahiert Dateizugriffe. Pfade sind absolute Pfade im Zielsystem.
type FS interface {
	ReadFile(p string) ([]byte, error)
	WriteFile(p string, data []byte, perm os.FileMode) error
	Remove(p string) error
	MkdirAll(p string, perm os.FileMode) error
	Glob(pattern string) ([]string, error)
	Symlink(target, link string) error
	// Rename benennt eine Datei um. Existiert das Ziel bereits, ist es ein Fehler (es wird nichts überschrieben).
	Rename(oldPath, newPath string) error
	Exists(p string) bool
}

type rootFS struct{ root string }

// NewRootFS liefert ein FS, dessen Pfade relativ zu root aufgelöst werden.
// Pfade, die root verlassen würden, führen zu einem Fehler.
func NewRootFS(root string) FS { return rootFS{root: filepath.Clean(root)} }

func (r rootFS) resolve(p string) (string, error) {
	raw := p
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("pfad %q verlässt das Wurzelverzeichnis", p)
		}
	}
	return filepath.Join(r.root, filepath.FromSlash(path.Clean(raw))), nil
}

func (r rootFS) ReadFile(p string) ([]byte, error) {
	full, err := r.resolve(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (r rootFS) WriteFile(p string, data []byte, perm os.FileMode) error {
	full, err := r.resolve(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), "."+filepath.Base(full)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, full)
}

func (r rootFS) Remove(p string) error {
	full, err := r.resolve(p)
	if err != nil {
		return err
	}
	return os.Remove(full)
}

func (r rootFS) MkdirAll(p string, perm os.FileMode) error {
	full, err := r.resolve(p)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, perm)
}

func (r rootFS) Glob(pattern string) ([]string, error) {
	full, err := r.resolve(pattern)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(full)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(r.root, m)
		if err != nil {
			return nil, err
		}
		out = append(out, "/"+filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out, nil
}

func (r rootFS) Symlink(target, link string) error {
	full, err := r.resolve(link)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	_ = os.Remove(full)
	return os.Symlink(target, full)
}

func (r rootFS) Exists(p string) bool {
	full, err := r.resolve(p)
	if err != nil {
		return false
	}
	_, err = os.Lstat(full)
	return err == nil
}

func (r rootFS) Rename(oldPath, newPath string) error {
	from, err := r.resolve(oldPath)
	if err != nil {
		return err
	}
	to, err := r.resolve(newPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return os.ErrExist
	}
	return os.Rename(from, to)
}
