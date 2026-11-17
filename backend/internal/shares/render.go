package shares

import (
	"fmt"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/storage"
)

// principals liefert die Samba-Schreibweise (Gruppen mit @) aller Einträge mit der gegebenen Stufe.
func principals(acl []ACL, access string) string {
	var out []string
	for _, a := range acl {
		if a.Access != access {
			continue
		}
		if a.Type == TypeGroup {
			out = append(out, "@"+a.Name)
		} else {
			out = append(out, a.Name)
		}
	}
	return strings.Join(out, " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func renderShare(s Share) []byte {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, "   "+format+"\n", a...) }
	fmt.Fprintf(&b, "%s\n[%s]\n", storage.ManagedHeader, s.Name)
	line("path = %s", s.Path())
	if s.Comment != "" {
		line("comment = %s", s.Comment)
	}
	line("browseable = %s", yesNo(s.Browsable))
	line("read only = %s", yesNo(!(s.Guest && s.GuestWrite)))
	line("guest ok = %s", yesNo(s.Guest))
	ro, rw, none := principals(s.ACL, AccessRO), principals(s.ACL, AccessRW), principals(s.ACL, AccessNone)
	if !s.Guest {
		line("valid users = %s", strings.TrimSpace(rw+" "+ro))
	} else if none != "" {
		line("invalid users = %s", none)
	}
	if ro != "" {
		line("read list = %s", ro)
	}
	if rw != "" && !(s.Guest && s.GuestWrite) {
		line("write list = %s", rw)
	}
	line("force group = users")
	line("create mask = 0664")
	line("force create mode = 0664")
	line("directory mask = 2775")
	line("force directory mode = 2775")
	var vfs []string
	if s.Fruit {
		vfs = append(vfs, "catia", "fruit", "streams_xattr")
	}
	if s.Recycle {
		vfs = append(vfs, "recycle")
	}
	if len(vfs) > 0 {
		line("vfs objects = %s", strings.Join(vfs, " "))
	}
	if s.Recycle {
		line("recycle:repository = .papierkorb/%%U")
		line("recycle:keeptree = yes")
		line("recycle:versions = yes")
		line("recycle:touch = yes")
		line("recycle:exclude_dir = .papierkorb")
	}
	return []byte(b.String())
}

// Render erzeugt alle Samba-Dateien (Dateiname → Inhalt) einschließlich der Include-Liste shares.conf.
func Render(st State) (map[string][]byte, error) {
	if err := st.Validate(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	var names []string
	for _, s := range st.Shares {
		out[s.FileName()] = renderShare(s)
		names = append(names, s.FileName())
	}
	sort.Strings(names)
	var inc strings.Builder
	inc.WriteString(storage.ManagedHeader + "\n")
	for _, n := range names {
		fmt.Fprintf(&inc, "include = %s/%s\n", ConfDir, n)
	}
	out["shares.conf"] = []byte(inc.String())
	return out, nil
}

// RenderExports erzeugt /etc/exports.d/betternas.exports: je Freigabe mit aktivem NFS eine Zeile.
func RenderExports(st State) []byte {
	var b strings.Builder
	b.WriteString(storage.ManagedHeader + "\n")
	list := append([]Share(nil), st.Shares...)
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	for _, s := range list {
		if !s.NFS.Enabled {
			continue
		}
		opts := []string{"rw", "sync", "no_subtree_check"}
		if s.NFS.ReadOnly {
			opts[0] = "ro"
		}
		if s.NFS.Squash == SquashAll {
			opts = append(opts, "all_squash", "anonuid=65534", "anongid=100")
		} else {
			opts = append(opts, "root_squash")
		}
		opts = append(opts, fmt.Sprintf("fsid=%d", FSID(s.Name)))
		o := "(" + strings.Join(opts, ",") + ")"
		b.WriteString(strings.ReplaceAll(s.Path(), " ", `\040`))
		for _, c := range s.NFS.Clients {
			b.WriteString(" " + c + o)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}
