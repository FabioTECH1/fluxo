package executiondomain

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ServiceIdentity is consumed with one-use application authority before payload
// launch. Boot/unit/adapter are held by the owning journal; PID1's invocation
// generation and the kernel domain identity prevent later loaded replacements
// from being mistaken for this same recorded persistent execution.
type ServiceIdentity struct {
	InvocationID string `json:"invocation_id"`
	Device       uint64 `json:"cgroup_device"`
	Inode        uint64 `json:"cgroup_inode"`
}

func (s *ServiceIdentity) Valid() bool {
	if s == nil || s.Inode == 0 {
		return false
	}
	raw, err := hex.DecodeString(s.InvocationID)
	return err == nil && len(raw) == 16 && strings.ToLower(s.InvocationID) == s.InvocationID
}
func CaptureServiceIdentity(unit string, p map[string]string) (*ServiceIdentity, error) {
	if unit == "" || filepath.Base(unit) != unit || p["ControlGroup"] != "/system.slice/"+unit {
		return nil, errors.New("persistent service must own its exact domain before authority consumption")
	}
	s := &ServiceIdentity{InvocationID: p["InvocationID"]}
	var st unix.Stat_t
	if err := unix.Lstat(filepath.Join("/sys/fs/cgroup/system.slice", unit), &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 {
		return nil, errors.New("persistent service domain is not protected root-owned cgroup state")
	}
	s.Device, s.Inode = uint64(st.Dev), st.Ino
	if !s.Valid() {
		return nil, errors.New("persistent service lacks an immutable PID1 invocation generation")
	}
	return s, nil
}
func ValidateServiceIdentity(saved *ServiceIdentity, unit string, p map[string]string) error {
	if !saved.Valid() {
		return errors.New("consumed persistent service is missing its saved kernel/PID1 identity")
	}
	if p["LoadState"] == "not-found" {
		return nil
	} // caller still proves expected hierarchy empty
	if p["InvocationID"] != saved.InvocationID {
		return errors.New("persistent service PID1 invocation generation changed")
	}
	var st unix.Stat_t
	err := unix.Lstat(filepath.Join("/sys/fs/cgroup/system.slice", unit), &st)
	if os.IsNotExist(err) && UnitQuiescent(PersistentV1, p) {
		return nil
	} // natural drain may prune an empty domain
	if err != nil {
		return err
	}
	if uint64(st.Dev) != saved.Device || st.Ino != saved.Inode {
		return errors.New("persistent service kernel execution domain changed")
	}
	return nil
}
