package docker

import "testing"

func TestParseHealth(t *testing.T) {
	cases := map[string]string{
		"Up 3 hours (healthy)":            "healthy",
		"Up 2 minutes (unhealthy)":        "unhealthy",
		"Up 5 seconds (health: starting)": "starting",
		"Up 3 hours":                      "",
		"Exited (0) 2 days ago":           "",
		"Restarting (1) 5 seconds ago":    "",
		"":                                "",
	}
	for in, want := range cases {
		if got := ParseHealth(in); got != want {
			t.Errorf("ParseHealth(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseContainersHealth(t *testing.T) {
	out := []byte(`{"ID":"abc","Names":"web","Image":"x","State":"running","Status":"Up 1 hour (unhealthy)","Ports":"","Labels":"","CreatedAt":"t"}` + "\n")
	list, err := ParseContainers(out)
	if err != nil || len(list) != 1 || list[0].Health != "unhealthy" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}
