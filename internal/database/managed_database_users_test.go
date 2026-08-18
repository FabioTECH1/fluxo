package database

import (
	"path/filepath"
	"testing"
)

func TestManagedDatabaseUserLifecycle(t *testing.T) {
	previousDB := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	reserved, err := BeginManagedDatabaseUser("mysql", "app_user", "127.0.0.1")
	if err != nil || !reserved {
		t.Fatalf("initial reservation = (%v, %v), want (true, nil)", reserved, err)
	}
	reserved, err = BeginManagedDatabaseUser("mysql", "app_user", "127.0.0.1")
	if err != nil || reserved {
		t.Fatalf("duplicate reservation = (%v, %v), want (false, nil)", reserved, err)
	}
	if err := ActivateManagedDatabaseUser("mysql", "app_user", "127.0.0.1"); err != nil {
		t.Fatalf("ActivateManagedDatabaseUser() error = %v", err)
	}
	state, err := ManagedDatabaseUserState("mysql", "app_user", "127.0.0.1")
	if err != nil || state != ManagedDatabaseUserActive {
		t.Fatalf("state = (%q, %v), want (%q, nil)", state, err, ManagedDatabaseUserActive)
	}
	if err := DeleteManagedDatabaseUser("mysql", "app_user", "127.0.0.1"); err != nil {
		t.Fatalf("DeleteManagedDatabaseUser() error = %v", err)
	}
	state, err = ManagedDatabaseUserState("mysql", "app_user", "127.0.0.1")
	if err != nil || state != "" {
		t.Fatalf("state after delete = (%q, %v), want empty", state, err)
	}
	if users, err := ListManagedDatabaseUsers("mysql", ManagedDatabaseUserPending); err != nil || len(users) != 0 {
		t.Fatalf("pending users after delete = (%v, %v), want none", users, err)
	}
}
