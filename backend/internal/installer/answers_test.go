package installer

import "testing"

func tzOK(tz string) bool { return tz == "Europe/Berlin" || tz == "UTC" }

func valid() Answers {
	a := Defaults()
	a.AdminUser, a.AdminPassword = "anna", "sicheres-pw"
	return a
}

func TestDefaultsAreValidExceptAdmin(t *testing.T) {
	if errs := valid().Validate(tzOK); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	errs := Defaults().Validate(tzOK)
	if len(errs) != 2 {
		t.Fatalf("defaults without admin: want 2 errors, got %v", errs)
	}
}

func static(a Answers) Answers {
	a.Net = NetConfig{Interface: "enp1s0", Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1"}}
	return a
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*Answers)
		field string
	}{
		{"hostname space", func(a *Answers) { a.Hostname = "Bet ter" }, "hostname"},
		{"hostname dash", func(a *Answers) { a.Hostname = "-x" }, "hostname"},
		{"user root", func(a *Answers) { a.AdminUser = "root" }, "adminUser"},
		{"user digit", func(a *Answers) { a.AdminUser = "1abc" }, "adminUser"},
		{"password short", func(a *Answers) { a.AdminPassword = "kurz" }, "adminPassword"},
		{"password = user", func(a *Answers) { a.AdminUser, a.AdminPassword = "annalena", "annalena" }, "adminPassword"},
		{"root size", func(a *Answers) { a.RootSizeGiB = 15 }, "rootSizeGiB"},
		{"gateway outside", func(a *Answers) { *a = static(*a); a.Net.Gateway = "10.0.0.1" }, "net.gateway"},
		{"address no prefix", func(a *Answers) { *a = static(*a); a.Net.Address = "192.168.1.20" }, "net.address"},
		{"no dns", func(a *Answers) { *a = static(*a); a.Net.DNS = nil }, "net.dns"},
		{"timezone", func(a *Answers) { a.Timezone = "Mars/Base" }, "timezone"},
		{"keymap", func(a *Answers) { a.Keymap = "dvorak" }, "keymap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := valid()
			c.mod(&a)
			errs := a.Validate(tzOK)
			if len(errs) != 1 || errs[0].Field != c.field {
				t.Fatalf("want exactly one error on %s, got %v", c.field, errs)
			}
		})
	}
}

func TestStaticNetValid(t *testing.T) {
	if errs := static(valid()).Validate(tzOK); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateFields(t *testing.T) {
	a := static(valid())
	a.Hostname = "X"
	a.Net.DNS = nil
	if errs := a.ValidateFields(tzOK, "net"); len(errs) != 1 || errs[0].Field != "net.dns" {
		t.Fatal(errs)
	}
	if errs := a.ValidateFields(tzOK, "hostname"); len(errs) != 1 {
		t.Fatal(errs)
	}
}
