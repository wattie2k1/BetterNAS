package users

import (
	"slices"
	"strconv"
	"strings"
)

// Parse liest /etc/passwd und /etc/group. Benutzer ab UID 1000 (ohne nobody) und Gruppen
// im BetterNAS-Bereich werden geliefert; all enthält jeden Benutzer- und Gruppennamen des Systems.
func Parse(passwd, group []byte) (users []User, groups []Group, all map[string]bool) {
	all = map[string]bool{}
	adminMembers := map[string]bool{}
	managed := map[string]bool{}
	memberOf := map[string][]string{}

	for _, line := range strings.Split(string(group), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 || f[0] == "" {
			continue
		}
		all[f[0]] = true
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		var members []string
		for _, m := range strings.Split(f[3], ",") {
			if m != "" {
				members = append(members, m)
			}
		}
		if f[0] == AdminGroup {
			for _, m := range members {
				adminMembers[m] = true
			}
		}
		if gid >= MinID && gid <= MaxID {
			managed[f[0]] = true
			slices.Sort(members)
			groups = append(groups, Group{Name: f[0], GID: gid, Members: append([]string{}, members...)})
			for _, m := range members {
				memberOf[m] = append(memberOf[m], f[0])
			}
		}
	}
	for _, line := range strings.Split(string(passwd), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 5 || f[0] == "" {
			continue
		}
		all[f[0]] = true
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < MinHumanUID || uid > MaxID {
			continue
		}
		gs := append([]string{}, memberOf[f[0]]...)
		slices.Sort(gs)
		users = append(users, User{
			Name: f[0], FullName: strings.SplitN(f[4], ",", 2)[0], UID: uid, Groups: gs,
			Admin: adminMembers[f[0]], System: uid < MinID,
		})
	}
	slices.SortFunc(users, func(a, b User) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(groups, func(a, b Group) int { return strings.Compare(a.Name, b.Name) })
	return users, groups, all
}
