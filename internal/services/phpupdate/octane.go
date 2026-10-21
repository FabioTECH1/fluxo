package phpupdate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

//go:embed octane_probe.php
var octaneProbe string

type OctaneDescriptor struct {
	Backend     string           `json:"backend"`
	Directory   string           `json:"directory"`
	Storage     string           `json:"storage"`
	Vendor      string           `json:"vendor"`
	AppName     string           `json:"app_name"`
	Fingerprint string           `json:"fingerprint"`
	Grace       string           `json:"grace"`
	RRBinary    string           `json:"rr_binary"`
	RRWorker    string           `json:"rr_worker"`
	RRSHA256    string           `json:"rr_sha256"`
	Extension   *NativeExtension `json:"extension"`
}
type NativeExtension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}
type octaneProbeResult struct {
	Descriptor OctaneDescriptor `json:"descriptor"`
	MasterPID  int              `json:"master_pid"`
	ManagerPID int              `json:"manager_pid"`
	Host       string           `json:"host"`
	Port       int              `json:"port"`
}
type octaneProcesses struct {
	Backend    string `json:"backend"`
	MasterPID  int    `json:"master_pid"`
	ManagerPID int    `json:"manager_pid"`
}

func validateNativeChanges(inventory Inventory, impact Impact) error {
	for _, consumer := range impact.Workers {
		if consumer.Octane == nil || consumer.Octane.Backend != "swoole" {
			continue
		}
		for _, change := range inventory.Changes {
			if change.Name == "php"+inventory.Version+"-swoole" {
				return errors.New("Updating the native Swoole extension requires separate runtime qualification; the managed PHP branch update is blocked")
			}
		}
	}
	return nil
}

// ValidateManagedOctane performs configuration-only compatibility preflight;
// providers are never booted to decide whether enabling a runtime is supported.
func ValidateManagedOctane(ctx context.Context, branch, directory string) error {
	_, err := probeOctane(ctx, branch, Consumer{Name: "managed Octane", Directory: directory}, false)
	return err
}

// VerifyManagedOctaneStartup is used before enablement changes web routing.
// FrankenPHP can be started, but remains outside APT patch maintenance.
func VerifyManagedOctaneStartup(ctx context.Context, branch string, id int, directory string, port int) error {
	c := Consumer{ID: id, Name: "managed Octane", Kind: "laravel_octane", Directory: directory, Instances: 1, Port: port, Command: octaneruntime.Command(branch, port)}
	probe, err := probeOctane(ctx, branch, c, false)
	if err != nil {
		return err
	}
	c.Octane = &probe.Descriptor
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		u, e := unitState(ctx, unitName(id, 1))
		err = e
		if err == nil && u.Active {
			if c.Octane.Backend == "frankenphp" {
				err = verifyListenerOwnership(ctx, u, port)
				if err == nil {
					err = octaneHTTP(ctx, port)
				}
			} else {
				err = octaneHealth(ctx, branch, c, u)
			}
			if err == nil {
				return nil
			}
		} else if err == nil {
			err = errors.New("Octane service is not running")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("Octane did not become ready; routing was not changed: %w", err)
}

func probeOctane(ctx context.Context, branch string, c Consumer, runtime bool) (octaneProbeResult, error) {
	var result octaneProbeResult
	if err := octaneruntime.Verify(); err != nil {
		return result, fmt.Errorf("Octane maintenance integration is unavailable: %w", err)
	}
	mode := "config"
	if runtime {
		mode = "runtime"
	}
	out, err := syscmd.RunApplicationProbe(ctx, 15*time.Second, "fluxo", c.Directory, "/usr/bin/php"+branch, "-d", "memory_limit=128M", "-r", strings.TrimPrefix(octaneProbe, "<?php"), "--", mode)
	if err != nil {
		return result, fmt.Errorf("Cannot inspect Octane for %s; verify its supported runtime configuration: %w", c.Name, err)
	}
	const prefix = "FLUXO_RUNTIME_JSON:"
	var payload string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			if payload != "" {
				return result, errors.New("Ambiguous Octane inspection output")
			}
			payload = strings.TrimPrefix(line, prefix)
		}
	}
	if payload == "" {
		return result, errors.New("Octane inspection returned no runtime descriptor")
	}
	if err = json.Unmarshal([]byte(payload), &result); err != nil {
		return result, err
	}
	directory, err := filepath.EvalSymlinks(c.Directory)
	if err != nil {
		return result, err
	}
	if result.Descriptor.Directory != directory || len(result.Descriptor.Fingerprint) != 64 || !filepath.IsAbs(result.Descriptor.Vendor) {
		return result, errors.New("Octane application identity could not be established")
	}
	return result, nil
}

func processArgs(pid int) ([]string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, errors.New("Process command is too large")
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00"), nil
}
func processParent(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	pos := strings.LastIndex(string(data), ")")
	if pos < 0 {
		return 0, errors.New("Invalid process stat")
	}
	fields := strings.Fields(string(data)[pos+1:])
	if len(fields) < 2 {
		return 0, errors.New("Invalid process parent")
	}
	return strconv.Atoi(fields[1])
}
func validateProcessUserAndEnvironment(pid int, uid uint32) error {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return err
	}
	validUID := false
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			values := strings.Fields(strings.TrimPrefix(line, "Uid:"))
			if len(values) != 4 {
				return errors.New("Unable to verify runtime credentials")
			}
			for _, value := range values {
				n, e := strconv.ParseUint(value, 10, 32)
				if e != nil || uint32(n) != uid {
					return errors.New("Runtime process has unexpected privileges")
				}
			}
			validUID = true
		}
	}
	if !validUID {
		return errors.New("Runtime process credentials are unavailable")
	}
	env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(env), "\x00") {
		key, value, _ := strings.Cut(entry, "=")
		if value != "" && (key == "PHPRC" || key == "PHP_INI_SCAN_DIR" || key == "LD_PRELOAD" || key == "LD_LIBRARY_PATH") {
			return errors.New("Custom runtime environment requires manual maintenance")
		}
	}
	return nil
}

func inspectOctaneProcesses(ctx context.Context, branch string, c Consumer, u unitSnapshot) (*octaneProcesses, error) {
	if !u.Active {
		return nil, nil
	}
	if c.Octane == nil {
		return nil, errors.New("Octane runtime has no approved descriptor")
	}
	if c.Octane.Backend == "frankenphp" {
		return nil, errors.New("FrankenPHP serves embedded PHP, which this APT operation cannot patch; its active controller requires separate runtime maintenance")
	}
	probe, err := probeOctane(ctx, branch, c, true)
	if err != nil {
		return nil, err
	}
	if digest(probe.Descriptor) != digest(c.Octane) || probe.Host != "127.0.0.1" || probe.Port != c.Port || probe.MasterPID == u.MainPID || u.PIDs[probe.MasterPID] == "" {
		return nil, errors.New("Running Octane identity differs from its approved runtime")
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		return nil, errors.New("Octane requires the non-root fluxo user")
	}
	controller, err := processArgs(u.MainPID)
	if err != nil {
		return nil, err
	}
	if len(controller) == 0 {
		return nil, errors.New("Missing Octane controller command")
	}
	controller[0] = "php" + branch
	if digest(controller) != digest(strings.Fields(c.Command)) {
		return nil, errors.New("Octane controller invocation differs from its managed command")
	}
	masterParent, err := processParent(probe.MasterPID)
	if err != nil || masterParent != u.MainPID {
		return nil, errors.New("Octane serving master is not the managed controller's child")
	}
	php := "/usr/bin/php" + branch
	workers := 0
	for pid, key := range u.PIDs {
		if err = validateProcessUserAndEnvironment(pid, credential.Uid); err != nil {
			return nil, err
		}
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			return nil, err
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		args, err := processArgs(pid)
		if err != nil || len(args) == 0 {
			return nil, errors.New("Octane process command is unavailable")
		}
		parent, err := processParent(pid)
		if err != nil {
			return nil, err
		}
		if pid != u.MainPID && u.PIDs[parent] == "" {
			return nil, errors.New("Octane contains a detached or unrecognized process")
		}
		if c.Octane.Backend == "swoole" {
			if err := verifyNativeExtension(ctx, branch, pid, c.Octane.Extension); err != nil {
				return nil, err
			}
			if exe != php {
				return nil, errors.New("Swoole uses an interpreter outside the approved PHP branch")
			}
			if pid != u.MainPID {
				title := strings.Join(args, " ")
				role := strings.TrimSuffix(strings.TrimPrefix(title, "swoole_http_server: "), " for "+c.Octane.AppName)
				if title != "swoole_http_server: "+role+" for "+c.Octane.AppName {
					return nil, errors.New("Unrecognized Swoole process role")
				}
				switch pid {
				case probe.MasterPID:
					if role != "master process" {
						return nil, errors.New("Swoole master role does not match its state")
					}
				case probe.ManagerPID:
					if role != "manager process" || parent != probe.MasterPID {
						return nil, errors.New("Swoole manager role does not match its parent")
					}
				default:
					if role != "worker process" && role != "task worker process" || parent != probe.ManagerPID {
						return nil, errors.New("Unrecognized Swoole worker")
					}
					workers++
				}
			}
		} else if c.Octane.Backend == "roadrunner" {
			if pid == probe.MasterPID {
				if err := verifyRoadRunnerExecutable(pid, c.Octane); err != nil {
					return nil, err
				}
				if exe != c.Octane.RRBinary || args[len(args)-1] != "serve" {
					return nil, errors.New("RoadRunner executable or command changed")
				}
				var command, address, config string
				for i := 1; i < len(args); i++ {
					if args[i] == "-o" && i+1 < len(args) {
						k, v, _ := strings.Cut(args[i+1], "=")
						if k == "server.command" {
							command = v
						}
						if k == "http.address" {
							address = v
						}
					}
					if args[i] == "-c" && i+1 < len(args) {
						config = args[i+1]
					}
				}
				if command != php+","+c.Octane.RRWorker || address != "127.0.0.1:"+strconv.Itoa(c.Port) || config != filepath.Join(c.Octane.Directory, ".rr.yaml") {
					return nil, errors.New("RoadRunner serving interpreter or configuration is unsupported")
				}
			} else {
				if exe != php {
					return nil, errors.New("RoadRunner PHP process uses an unsupported interpreter")
				}
				if pid != u.MainPID {
					if len(args) != 2 || args[1] != c.Octane.RRWorker || parent != probe.MasterPID {
						return nil, errors.New("Custom RoadRunner worker requires manual maintenance")
					}
					workers++
				}
			}
		} else {
			return nil, errors.New("Unsupported Octane backend")
		}
		if now, e := processKey(pid); e != nil || now != key {
			return nil, errors.New("Octane process changed during inspection")
		}
	}
	if workers == 0 {
		return nil, errors.New("Octane serving workers have not become ready")
	}
	if err = verifyListenerOwnership(ctx, u, c.Port); err != nil {
		return nil, err
	}
	return &octaneProcesses{Backend: c.Octane.Backend, MasterPID: probe.MasterPID, ManagerPID: probe.ManagerPID}, nil
}

// Bind the approved on-disk binary to the executable actually running. A
// replaced rr pathname must never qualify the old process for native drainage.
func verifyRoadRunnerExecutable(pid int, descriptor *OctaneDescriptor) error {
	if descriptor == nil || len(descriptor.RRSHA256) != 64 {
		return errors.New("RoadRunner executable has no approved identity")
	}
	path := fmt.Sprintf("/proc/%d/exe", pid)
	target, err := os.Readlink(path)
	if err != nil || target != descriptor.RRBinary || strings.HasSuffix(target, " (deleted)") {
		return errors.New("Running RoadRunner executable was replaced or differs from its approved path")
	}
	live, err := os.Open(path)
	if err != nil {
		return err
	}
	defer live.Close()
	info, err := live.Stat()
	if err != nil {
		return err
	}
	disk, err := os.Stat(descriptor.RRBinary)
	if err != nil || !os.SameFile(info, disk) || !info.Mode().IsRegular() || info.Size() > 128*1024*1024 {
		return errors.New("Running RoadRunner inode differs from its approved executable")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(live, 128*1024*1024+1)); err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != descriptor.RRSHA256 {
		return errors.New("Running RoadRunner bytes differ from its approved executable")
	}
	// Recheck after hashing so an atomic replacement during inspection fails.
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(info, current) {
		return errors.New("Running RoadRunner executable changed during inspection")
	}
	disk, err = os.Stat(descriptor.RRBinary)
	if err != nil || !os.SameFile(info, disk) {
		return errors.New("RoadRunner executable was replaced during inspection")
	}
	return nil
}

func verifyNativeExtension(ctx context.Context, branch string, pid int, extension *NativeExtension) error {
	if extension == nil || extension.Name != "swoole" || extension.Version != "6.2.3" || len(extension.SHA256) != 64 {
		return errors.New("Native Swoole identity is not qualified")
	}
	if err := packageOwns(ctx, extension.File, branch); err != nil {
		return err
	}
	info, err := os.Stat(extension.File)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Native Swoole inode unavailable")
	}
	bytes, err := os.ReadFile(extension.File)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes)) != extension.SHA256 {
		return errors.New("Native Swoole bytes differ from the approved descriptor")
	}
	maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return err
	}
	found := false
	for _, line := range strings.Split(string(maps), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		path := strings.Join(fields[5:], " ")
		if strings.HasSuffix(path, "/swoole.so") || strings.HasSuffix(path, "/swoole.so (deleted)") {
			inode, e := strconv.ParseUint(fields[4], 10, 64)
			device := fmt.Sprintf("%02x:%02x", unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev)))
			if e != nil || path != extension.File || inode != st.Ino || fields[3] != device {
				return errors.New("Live Swoole mapping differs from its approved native extension")
			}
			found = true
		}
	}
	if !found {
		return errors.New("Live Swoole extension mapping unavailable")
	}
	return nil
}
