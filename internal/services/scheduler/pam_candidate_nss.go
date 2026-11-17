package scheduler

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// systemd's NSS glue requests identity/membership records, never StartUnit or
// process attachment. Reject custom record drop-ins and provider registrations;
// accepted standard provider endpoints remain inside the explicit authentic
// vendor-systemd platform boundary, rather than trusting arbitrary NSS plugins.
func (p *pamCandidateInspection) userdbConfiguration() error {
	for _, directory := range []string{"/etc/userdb", "/run/userdb", "/run/host/userdb", "/usr/local/lib/userdb", "/usr/lib/userdb"} {
		if _, err := p.canonical(directory); os.IsNotExist(err) {
			p.report.Files[directory] = "absent"
			continue
		} else if err != nil {
			return err
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("unreviewed systemd userdb record/drop-in authority: %s", directory)
		}
		p.report.Files[directory+"/*"] = "empty"
	}
	directory := "/run/systemd/userdb"
	if _, err := p.canonical(directory); os.IsNotExist(err) {
		p.report.Files[directory] = "absent"
		return nil
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		switch entry.Name() {
		case "io.systemd.DynamicUser", "io.systemd.Multiplexer", "io.systemd.NameServiceSwitch", "io.systemd.DropIn":
		default:
			return errors.New("unreviewed systemd NSS provider endpoint")
		}
		path := directory + "/" + entry.Name()
		observation, err := pamCandidateObserve(path)
		if err != nil {
			return err
		}
		// A public query socket may be mode0666. Its protected root-owned directory
		// controls endpoint replacement; socket write bits grant requests, not code.
		if observation.Mode&unix.S_IFMT != unix.S_IFSOCK || observation.UID != 0 || observation.Nlink != 1 {
			return errors.New("unprotected systemd NSS provider endpoint")
		}
		if err = p.remember(path, observation); err != nil {
			return err
		}
		names = append(names, entry.Name())
	}
	p.report.Files[directory+"/*"] = strings.Join(names, "\n")
	return nil
}
