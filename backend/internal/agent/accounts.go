package agent

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/users"
)

// UserCreate beschreibt ein neues NAS-Benutzerkonto.
type UserCreate struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Password string   `json:"password"`
	Groups   []string `json:"groups"`
	Admin    bool     `json:"admin"`
}

// UserUpdate ändert Namen, Gruppen und Rolle eines Kontos.
type UserUpdate struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Groups   []string `json:"groups"`
	Admin    bool     `json:"admin"`
}

type accountFiles struct {
	passwd, group []byte
	accounts      users.Accounts
	all           map[string]bool
}

func readAccounts(d Deps) (accountFiles, error) {
	pw, err := d.FS.ReadFile("/etc/passwd")
	if err != nil {
		return accountFiles{}, err
	}
	gr, err := d.FS.ReadFile("/etc/group")
	if err != nil {
		return accountFiles{}, err
	}
	us, gs, all := users.Parse(pw, gr)
	return accountFiles{passwd: pw, group: gr, accounts: users.Accounts{Users: us, Groups: gs}, all: all}, nil
}

// ListAccounts liefert Benutzer und BetterNAS-Gruppen.
func ListAccounts(_ context.Context, d Deps) (users.Accounts, error) {
	a, err := readAccounts(d)
	if err != nil {
		return users.Accounts{}, err
	}
	if a.accounts.Users == nil {
		a.accounts.Users = []users.User{}
	}
	if a.accounts.Groups == nil {
		a.accounts.Groups = []users.Group{}
	}
	return a.accounts, nil
}

// uidOf sucht die UID eines Kontos in /etc/passwd.
func (a accountFiles) uidOf(name string) (int, bool) {
	for _, line := range strings.Split(string(a.passwd), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 3 && f[0] == name {
			uid, err := strconv.Atoi(f[2])
			return uid, err == nil
		}
	}
	return 0, false
}

// manageableUser stellt sicher, dass name ein von BetterNAS verwaltbares Konto ist.
func (a accountFiles) manageableUser(name string) error {
	uid, ok := a.uidOf(name)
	switch {
	case !ok:
		return apierr.New(apierr.NotFound, "Benutzer nicht gefunden")
	case uid < users.MinHumanUID || uid > users.MaxID || name == "betternas":
		return apierr.New(apierr.Forbidden, "Dieses Systemkonto kann hier nicht verwaltet werden")
	}
	return nil
}

func (a accountFiles) managedGroups(names []string) error {
	for _, g := range names {
		if !slices.ContainsFunc(a.accounts.Groups, func(x users.Group) bool { return x.Name == g }) {
			return apierr.New(apierr.InvalidParams, "Unbekannte Gruppe: "+g).With("field", "groups")
		}
	}
	return nil
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// CreateUser legt ein NAS-Benutzerkonto samt Samba-Passwort an. Scheitert ein Schritt, wird das Konto wieder entfernt.
func CreateUser(ctx context.Context, d Deps, in UserCreate) error {
	if err := users.ValidateName(in.Name); err != nil {
		return err
	}
	if err := users.ValidatePassword(in.Name, in.Password); err != nil {
		return err
	}
	if err := users.ValidateFullName(in.FullName); err != nil {
		return err
	}
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if a.all[in.Name] {
		return apierr.New(apierr.Conflict, "Der Name wird auf diesem System bereits verwendet").With("field", "name")
	}
	groups := dedupe(in.Groups)
	if err := a.managedGroups(groups); err != nil {
		return err
	}
	if in.Admin {
		groups = append(groups, users.AdminGroup)
	}
	args := []string{"-K", "UID_MIN=10000", "-K", "UID_MAX=59999", "-M", "-d", "/nonexistent", "-s", "/usr/sbin/nologin", "-N", "-g", "users", "-c", in.FullName}
	if len(groups) > 0 {
		args = append(args, "-G", strings.Join(groups, ","))
	}
	if _, err := run(ctx, d.Runner, nil, "useradd", append(args, in.Name)...); err != nil {
		return err
	}
	if err := setPasswords(ctx, d, in.Name, in.Password); err != nil {
		_, _ = d.Runner.Run(ctx, nil, "smbpasswd", "-x", in.Name)
		_, _ = d.Runner.Run(ctx, nil, "userdel", in.Name)
		return err
	}
	return nil
}

func setPasswords(ctx context.Context, d Deps, name, pw string) error {
	if _, err := run(ctx, d.Runner, []byte(name+":"+pw+"\n"), "chpasswd"); err != nil {
		return err
	}
	_, err := run(ctx, d.Runner, []byte(pw+"\n"+pw+"\n"), "smbpasswd", "-a", "-s", name)
	return err
}

// UpdateUser ändert vollständigen Namen, Gruppen und Admin-Recht. Fremde Gruppen (z. B. sudo) bleiben erhalten.
func UpdateUser(ctx context.Context, d Deps, in UserUpdate) error {
	if err := users.ValidateFullName(in.FullName); err != nil {
		return err
	}
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if err := a.manageableUser(in.Name); err != nil {
		return err
	}
	selected := dedupe(in.Groups)
	if err := a.managedGroups(selected); err != nil {
		return err
	}
	var groups []string
	for _, line := range strings.Split(string(a.group), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 || f[0] == users.AdminGroup || !slices.Contains(strings.Split(f[3], ","), in.Name) {
			continue
		}
		if gid, err := strconv.Atoi(f[2]); err == nil && (gid < users.MinID || gid > users.MaxID) {
			groups = append(groups, f[0])
		}
	}
	groups = append(groups, selected...)
	if in.Admin {
		groups = append(groups, users.AdminGroup)
	}
	_, err = run(ctx, d.Runner, nil, "usermod", "-c", in.FullName, "-G", strings.Join(groups, ","), in.Name)
	return err
}

// SetUserPassword setzt das Linux- und das Samba-Passwort.
func SetUserPassword(ctx context.Context, d Deps, name, pw string) error {
	if err := users.ValidatePassword(name, pw); err != nil {
		return err
	}
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if err := a.manageableUser(name); err != nil {
		return err
	}
	return setPasswords(ctx, d, name, pw)
}

// DeleteUser entfernt Konto und Samba-Eintrag.
func DeleteUser(ctx context.Context, d Deps, name string) error {
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if err := a.manageableUser(name); err != nil {
		return err
	}
	_, _ = d.Runner.Run(ctx, nil, "smbpasswd", "-x", name) // war evtl. nie in Samba
	_, err = run(ctx, d.Runner, nil, "userdel", name)
	return err
}

func (a accountFiles) managedGroup(name string) error {
	if slices.ContainsFunc(a.accounts.Groups, func(g users.Group) bool { return g.Name == name }) {
		return nil
	}
	if a.all[name] {
		return apierr.New(apierr.Forbidden, "Diese Systemgruppe kann hier nicht verwaltet werden")
	}
	return apierr.New(apierr.NotFound, "Gruppe nicht gefunden")
}

// CreateGroup legt eine Gruppe im BetterNAS-ID-Bereich an.
func CreateGroup(ctx context.Context, d Deps, name string) error {
	if err := users.ValidateName(name); err != nil {
		return err
	}
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if a.all[name] {
		return apierr.New(apierr.Conflict, "Der Name wird auf diesem System bereits verwendet").With("field", "name")
	}
	_, err = run(ctx, d.Runner, nil, "groupadd", "-K", "GID_MIN=10000", "-K", "GID_MAX=59999", name)
	return err
}

// DeleteGroup entfernt eine BetterNAS-Gruppe.
func DeleteGroup(ctx context.Context, d Deps, name string) error {
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if err := a.managedGroup(name); err != nil {
		return err
	}
	_, err = run(ctx, d.Runner, nil, "groupdel", name)
	return err
}

// SetGroupMembers ersetzt die Mitgliederliste einer BetterNAS-Gruppe.
func SetGroupMembers(ctx context.Context, d Deps, name string, members []string) error {
	a, err := readAccounts(d)
	if err != nil {
		return err
	}
	if err := a.managedGroup(name); err != nil {
		return err
	}
	members = dedupe(members)
	for _, m := range members {
		if !slices.ContainsFunc(a.accounts.Users, func(u users.User) bool { return u.Name == m }) {
			return apierr.New(apierr.InvalidParams, "Unbekannter Benutzer: "+m).With("field", "members")
		}
	}
	_, err = run(ctx, d.Runner, nil, "gpasswd", "-M", strings.Join(members, ","), name)
	return err
}
