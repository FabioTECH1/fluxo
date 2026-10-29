package maintenancetask

import (
	"context"
	"errors"
	"os"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

func composerLease() (func(), error) {
	fd, err := unix.Open("/run/lock/fluxo-composer.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 {
		unix.Close(fd)
		return nil, errors.New("Unsafe Composer maintenance lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}

func runComposer(ctx context.Context, php string) error {
	return syscmd.RunMaintenance(ctx, "", os.Stdout, []string{"COMPOSER_ALLOW_SUPERUSER=1"}, php, "/usr/local/bin/composer", "self-update", "--2", "--stable", "--no-interaction", "--no-ansi")
}
