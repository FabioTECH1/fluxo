package nodeupdate

import (
	"context"
	"testing"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

func TestNodeReleaseRejectsInvalidIdentityAndForeignOwner(t *testing.T) {
	m := testManager(t)
	for _, g := range []Generation{
		{ID: "../foreign"},
		{ID: uuid.NewString(), Plan: Plan{Impact: Impact{Units: []Unit{{ID: 1, Name: "../foreign"}}}}},
		{ID: uuid.NewString(), Plan: Plan{Impact: Impact{Units: []Unit{{ID: 0, Name: "fluxo-daemon-0.service"}}}}},
	} {
		if err := m.release(context.Background(), g); err == nil {
			t.Fatal("invalid release identity accepted", g)
		}
	}
	u := Unit{ID: 209201, Name: "fluxo-daemon-209201.service"}
	owner := uuid.NewString()
	if _, err := database.DB.Exec("INSERT INTO node_update_barriers(unit,generation_id) VALUES(?,?)", u.Name, owner); err != nil {
		t.Fatal(err)
	}
	g := Generation{ID: uuid.NewString(), Plan: Plan{Impact: Impact{Units: []Unit{u}}}}
	if err := m.release(context.Background(), g); err == nil {
		t.Fatal("foreign barrier receipt accepted")
	}
	var retained string
	if err := database.DB.QueryRow("SELECT generation_id FROM node_update_barriers WHERE unit=?", u.Name).Scan(&retained); err != nil || retained != owner {
		t.Fatal("foreign receipt changed", retained, err)
	}
}
