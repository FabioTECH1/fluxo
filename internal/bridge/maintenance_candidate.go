package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// The installer creates this root-only, one-use handoff AFTER independent
// release-provenance verification (or explicit local-build SHA selection).
// An artifact's self-reported version/digest is never that authentication.
// This protocol authorizes only maintenance-registration, not activation or a
// generic privileged command. Ordinary mutation keeps the installed-path rule.
type maintenanceCandidateHandoff struct {
	Format            int      `json:"format"`
	Authority         string   `json:"authority"`
	ID                string   `json:"id"`
	ArtifactSHA256    string   `json:"artifact_sha256"`
	Version           string   `json:"version"`
	Architecture      string   `json:"architecture"`
	InstallationID    string   `json:"installation_id"`
	SourceFloorSHA256 string   `json:"source_floor_sha256"`
	OperationID       string   `json:"operation_id"`
	ParentPID         int      `json:"parent_pid"`
	ParentStart       string   `json:"parent_start"`
	Boot              string   `json:"boot"`
	Expires           int64    `json:"expires"`
	Action            string   `json:"action"`
	Expectations      []string `json:"expectations"`
}

type maintenanceCandidateAdmission struct {
	Handoff       maintenanceCandidateHandoff
	UsedPath      string
	HandoffSHA256 string
}

func candidateProcessStart(pid int) (string, error) {
	raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", errors.New("malformed installer parent identity")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return "", errors.New("short installer parent identity")
	}
	return fields[19], nil
}

func (a *maintenanceCandidateAdmission) revalidate(version string) error {
	h := a.Handoff
	if time.Now().Unix() >= h.Expires {
		return errors.New("installer maintenance authorization expired; use a fresh authenticated handoff for exact recovery")
	}
	if os.Getppid() != h.ParentPID {
		return errors.New("installer handoff parent changed")
	}
	var e error
	var parent unix.Stat_t
	if e = unix.Stat(fmt.Sprintf("/proc/%d", h.ParentPID), &parent); e != nil || parent.Uid != 0 {
		return errors.New("installer handoff parent is unavailable or unprivileged")
	}
	start, e := candidateProcessStart(h.ParentPID)
	if e != nil || start != h.ParentStart {
		return errors.New("installer parent process generation changed")
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil || strings.TrimSpace(string(boot)) != h.Boot {
		return errors.New("installer handoff boot changed")
	}
	image, e := readCertifyingExecutable()
	if e != nil {
		return e
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	if executable != "/usr/local/bin/.fluxo-maintenance-candidate-"+h.ID || digest(image) != h.ArtifactSHA256 || h.Version != version || h.Architecture != runtime.GOARCH {
		return errors.New("installer handoff differs from the executing verified image")
	}
	raw, e := readProtected(a.UsedPath, 64<<10)
	if e != nil {
		return e
	}
	if digest(raw) != a.HandoffSHA256 {
		return errors.New("consumed installer handoff changed")
	}
	return nil
}

func InstallerMaintenanceRegistrationCommand(ctx context.Context, dataDir, version, handoffPath, action string, args []string) (any, error) {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return nil, errors.New("installer maintenance requires root and the default installed scope")
	}
	id := strings.TrimSuffix(filepath.Base(handoffPath), ".json")
	parsed, e := uuid.Parse(id)
	if e != nil || parsed.String() != id || handoffPath != filepath.Join("/run/fluxo-maintenance-requalification", id+".json") {
		return nil, errors.New("invalid installer maintenance handoff path")
	}
	raw, e := readProtected(handoffPath, 64<<10)
	if e != nil {
		return nil, e
	}
	info, e := protectedInfo(handoffPath)
	if e != nil || info.Mode().Perm() != 0600 {
		return nil, errors.New("installer handoff must be private")
	}
	var h maintenanceCandidateHandoff
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&h); e != nil {
		return nil, e
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return nil, errors.New("trailing installer handoff data")
	}
	if e = validateMaintenanceHandoffScope(h, id, version, action, args, time.Now().Unix()); e != nil {
		return nil, e
	}

	used := handoffPath + ".used"
	// Atomically consume before operation admission. Failure/retry requires a new
	// release-authenticated handoff; exact recovery keeps the original plan UUID.
	if e = unix.Renameat2(unix.AT_FDCWD, handoffPath, unix.AT_FDCWD, used, unix.RENAME_NOREPLACE); e != nil {
		return nil, e
	}
	if e = syncActivationDirectory(filepath.Dir(used)); e != nil {
		return nil, e
	}
	admission := &maintenanceCandidateAdmission{Handoff: h, UsedPath: used, HandoffSHA256: digest(raw)}
	if e = admission.revalidate(version); e != nil {
		return nil, e
	}
	deadlineCtx, cancel := context.WithDeadline(ctx, time.Unix(h.Expires, 0))
	defer cancel()
	return maintenanceRegistrationCommand(deadlineCtx, dataDir, version, action, args, admission)
}

func validateMaintenanceHandoffScope(h maintenanceCandidateHandoff, id, version, action string, args []string, now int64) error {
	if h.Format != 1 || h.ID != id || h.Authority != "verified-release" && h.Authority != "explicit-local-sha256" || !validSHA256(h.ArtifactSHA256) ||
		h.Expires <= now || h.Expires > now+120 || h.Action != action || !reflect.DeepEqual(h.Expectations, args) ||
		len(h.InstallationID) != 32 || !validSHA256(h.InstallationID+h.InstallationID) || h.Version != version || h.Architecture != runtime.GOARCH || !validSHA256(h.SourceFloorSHA256) || h.ParentPID <= 1 || h.ParentStart == "" {
		return errors.New("installer handoff scope, provenance or expiry is invalid")
	}
	if _, e := strconv.ParseUint(h.ParentStart, 10, 64); e != nil {
		return e
	}
	operation, e := uuid.Parse(h.OperationID)
	if e != nil || operation.String() != h.OperationID {
		return errors.New("installer handoff operation is invalid")
	}
	switch action {
	case "inspect":
		if len(args) != 0 {
			return errors.New("inspection handoff has mutation arguments")
		}
	case "requalify":
		if len(args) != 2 || !validSHA256(args[0]) || !validSHA256(args[1]) {
			return errors.New("requalification handoff expectations are invalid")
		}
	case "recover":
		if len(args) != 1 || args[0] != h.OperationID {
			return errors.New("recovery handoff differs from its operation")
		}
	default:
		return errors.New("installer handoff is not a maintenance registration command")
	}

	return nil
}
