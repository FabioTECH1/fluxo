package scheduler

// Copy this test-only file into an immutable v0.4.36 checkout when building the
// old helper acceptance fixture. It executes that checkout's actual mutations;
// it is not a mock of its JSON serialization or registration behavior.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type oldMutationRecipe struct {
	Entry  Entry
	Legacy string
	Action string
}

func init() {
	if len(os.Args) != 3 || !strings.HasPrefix(os.Args[1], "fixture-scheduler-") {
		return
	}
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		os.Exit(2)
	}
	path := os.Args[2]
	if !strings.HasPrefix(path, "/var/lib/fluxo-scheduler-acceptance-") || filepath.Clean(path) != path {
		os.Exit(2)
	}
	if err := trustedFile(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var recipe oldMutationRecipe
	if err = json.Unmarshal(b, &recipe); err != nil {
		panic(err)
	}
	stateDir = filepath.Dir(path)
	cronDir = filepath.Join(stateDir, "cron")
	databasePath = recipe.Entry.Database
	switch os.Args[1] {
	case "fixture-scheduler-materialize":
		e, ok := Qualify(recipe.Entry.ID, recipe.Entry.SiteID, recipe.Entry.Directory, "* * * * *", recipe.Entry.Command, "fluxo")
		if !ok {
			os.Exit(2)
		}
		err = Materialize(context.Background(), e, recipe.Legacy, true)
	case "fixture-scheduler-disable":
		err = Disable(context.Background(), recipe.Entry.ID)
		if err == nil {
			err = os.Remove(CronPath(recipe.Entry.ID))
		}
	default:
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
