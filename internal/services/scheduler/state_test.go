package scheduler

import "testing"

func TestQualifyPinsInterpreterAndExactCommand(t *testing.T) {
	dir := "/home/fluxo/app/current"
	good := "php8.4 " + dir + "/artisan schedule:run"
	if e, ok := Qualify(1, 2, dir, "* * * * *", good, "fluxo"); !ok || e.Version != "8.4" {
		t.Fatal("generated Scheduler rejected")
	}
	for _, command := range []string{"php " + dir + "/artisan schedule:run", good + " && id", good + " --verbose", "/usr/local/bin/" + good, "php8.4 artisan schedule:run"} {
		if _, ok := Qualify(1, 2, dir, "* * * * *", command, "fluxo"); ok {
			t.Fatalf("custom command accepted: %q", command)
		}
	}
	if _, ok := Qualify(1, 0, dir, "* * * * *", good, "fluxo"); ok {
		t.Fatal("global Scheduler adopted")
	}
	if _, ok := Qualify(1, 2, dir, "* * * * *", good, "root"); ok {
		t.Fatal("root application accepted")
	}
	if _, ok := Qualify(1, 2, dir, "*/2 * * * *", good, "fluxo"); ok {
		t.Fatal("custom schedule adopted")
	}
}
