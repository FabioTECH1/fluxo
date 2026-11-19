package nginx

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

// TrafficWorker binds a PID to its boot and Linux process start tick. Persist
// the complete value: a reused PID, including after reboot, is not this worker.
type TrafficWorker struct {
	PID       int    `json:"pid"`
	StartTime string `json:"start_time"`
	BootID    string `json:"boot_id"`
}

type trafficProcesses struct {
	Master  TrafficWorker
	Workers []TrafficWorker
}

func trafficProcessIdentity(pid int, boot string) (TrafficWorker, error) {
	if pid <= 1 {
		return TrafficWorker{}, errors.New("invalid Nginx process ID")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return TrafficWorker{}, err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return TrafficWorker{}, errors.New("invalid Nginx process identity")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return TrafficWorker{}, errors.New("short Nginx process identity")
	}
	return TrafficWorker{pid, fields[19], boot}, nil
}

func trafficProcessSnapshot(ctx context.Context) (trafficProcesses, error) {
	raw, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", "nginx", "--property=MainPID", "--value")
	if err != nil {
		return trafficProcesses{}, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pid <= 1 {
		return trafficProcesses{}, errors.New("traffic analytics requires a running systemd Nginx master")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return trafficProcesses{}, err
	}
	master, err := trafficProcessIdentity(pid, strings.TrimSpace(string(boot)))
	if err != nil {
		return trafficProcesses{}, err
	}
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !strings.HasPrefix(string(cmd), "nginx: master process ") {
		return trafficProcesses{}, errors.New("systemd MainPID is not an Nginx master")
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return trafficProcesses{}, err
	}
	if executable != nginxBinaryPath {
		return trafficProcesses{}, errors.New("traffic analytics requires the supported installed Nginx executable")
	}
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if err != nil {
		return trafficProcesses{}, err
	}
	snapshot := trafficProcesses{Master: master}
	for _, child := range strings.Fields(string(children)) {
		childPID, err := strconv.Atoi(child)
		if err != nil {
			return trafficProcesses{}, err
		}
		cmd, err := os.ReadFile(filepath.Join("/proc", child, "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return trafficProcesses{}, err
		}
		if !strings.HasPrefix(string(cmd), "nginx: worker process") {
			continue
		}
		worker, err := trafficProcessIdentity(childPID, master.BootID)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return trafficProcesses{}, err
		}
		snapshot.Workers = append(snapshot.Workers, worker)
	}
	if len(snapshot.Workers) == 0 {
		return trafficProcesses{}, errors.New("no running Nginx workers could be identified")
	}
	return snapshot, nil
}

// TrafficWorkersDrained is bounded and read-only. Unknown identity/permission
// failures remain pending; they are never reported as successful retirement.
func TrafficWorkersDrained(ctx context.Context, workers []TrafficWorker) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(workers) > 4096 {
		return false, errors.New("traffic worker drain set exceeded its limit")
	}
	if len(workers) == 0 {
		return true, nil
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return false, err
	}
	for _, worker := range workers {
		if worker.PID <= 1 || worker.StartTime == "" || worker.BootID == "" {
			return false, errors.New("invalid retained traffic worker identity")
		}
		if worker.BootID != strings.TrimSpace(string(boot)) {
			continue
		}
		current, err := trafficProcessIdentity(worker.PID, worker.BootID)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if current == worker {
			return false, nil
		}
	}
	return true, nil
}

func probeTrafficActivation(ctx context.Context, socket, operation string) error {
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var last error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://fluxo-traffic-control/activation", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 256))
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && string(body) == operation {
				return nil
			}
			err = fmt.Errorf("activation marker has not reached Nginx workers")
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("Nginx loaded-config activation was not confirmed: %w", last)
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// ReopenTrafficLogs asks the existing Nginx master to reopen its log files after
// a safe rename. Reopen does not apply disk configuration or terminate clients.
// Nginx may assign the reopened file to its worker UID; the root-owned private
// parent directory must remain the authority for file creation and replacement.
//
// Deprecated: root-private traffic source directories prevent unprivileged
// workers from reopening the new pathname. Use a validated same-generation
// ApplyTraffic reload and TrafficLogRetired before retiring the old inode.
func ReopenTrafficLogs(ctx context.Context) error {
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "kill", "--signal=USR1", "--kill-who=main", "nginx")
	if err != nil {
		return fmt.Errorf("reopen Nginx traffic logs: %w", err)
	}
	return nil
}

// A fresh worker can answer the activation probe before an older worker has
// processed QUIT. Wait for the old worker to acknowledge graceful shutdown so
// an immediate ON request cannot be accepted by the preceding OFF generation.
// Existing requests remain alive; their PID identities are returned for drain.
func awaitTrafficWorkerShutdown(ctx context.Context, workers []TrafficWorker) error {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	bootID := strings.TrimSpace(string(boot))
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		acknowledged := true
		for _, worker := range workers {
			if worker.BootID != bootID {
				continue
			}
			identity, err := trafficProcessIdentity(worker.PID, bootID)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if identity != worker {
				continue
			}
			cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", worker.PID))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !strings.HasPrefix(string(cmdline), "nginx: worker process is shutting down") {
				acknowledged = false
				break
			}
			listening, err := trafficWorkerHasListeners(worker.PID)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if listening {
				acknowledged = false
				break
			}
		}
		// Observe kernel listener state as well as Nginx's shutdown title; a
		// descheduled worker between those operations cannot create a false proof.
		if acknowledged {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Nginx old workers have not acknowledged graceful shutdown; activation remains pending")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// TrafficLogRetired proves that no Nginx process still has the named source
// inode open. USR1 is asynchronous, so rotation must call this before replacing
// a drained .1 file. This bounded scan is intentionally not a polling hot path.
func TrafficLogRetired(ctx context.Context, path string) (bool, error) {
	return trafficLogRetired(ctx, path, true, nginxBinaryPath)
}

func trafficLogRetired(ctx context.Context, path string, strict bool, binary string) (bool, error) {
	file, err := trafficOpenFile(filepath.Dir(path), filepath.Base(path), unix.O_RDONLY, 0, strict, false)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	source, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("traffic log has no stable file identity")
	}
	proc, err := os.Open("/proc")
	if err != nil {
		return false, err
	}
	names, err := proc.Readdirnames(8193)
	proc.Close()
	if err != nil && err != io.EOF {
		return false, err
	}
	if len(names) > 8192 {
		return false, errors.New("traffic log retirement process scan exceeded its limit")
	}
	processCount, fdCount := 0, 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, err := strconv.Atoi(name); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", name, "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !strings.HasPrefix(string(cmdline), "nginx: ") {
			continue
		}
		executable, err := os.Readlink(filepath.Join("/proc", name, "exe"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if executable != binary && executable != binary+" (deleted)" {
			continue
		}
		processCount++
		if processCount > 512 {
			return false, errors.New("traffic log retirement Nginx process scan exceeded its limit")
		}
		descriptorPath := filepath.Join("/proc", name, "fd")
		directory, err := os.Open(descriptorPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		descriptors, err := directory.Readdirnames(8193)
		directory.Close()
		if err != nil && err != io.EOF {
			return false, err
		}
		if len(descriptors) > 8192 {
			return false, errors.New("traffic log retirement descriptor scan exceeded its per-process limit")
		}
		for _, descriptor := range descriptors {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			fdCount++
			if fdCount > 65536 {
				return false, errors.New("traffic log retirement descriptor scan exceeded its total limit")
			}
			opened, err := os.Stat(filepath.Join(descriptorPath, descriptor))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return false, err
			}
			identity, ok := opened.Sys().(*syscall.Stat_t)
			if !ok {
				return false, errors.New("Nginx descriptor has no stable identity")
			}
			if identity.Dev == source.Dev && identity.Ino == source.Ino {
				return false, nil
			}
		}
	}
	// The path may have been replaced by an operator during the scan. Do not
	// certify a different inode using the original inode's descriptor evidence.
	current, err := trafficOpenFile(filepath.Dir(path), filepath.Base(path), unix.O_RDONLY, 0, strict, false)
	if err != nil {
		return false, err
	}
	defer current.Close()
	currentInfo, err := current.Stat()
	if err != nil {
		return false, err
	}
	identity, ok := currentInfo.Sys().(*syscall.Stat_t)
	if !ok || identity.Dev != source.Dev || identity.Ino != source.Ino {
		return false, errors.New("traffic log changed during retirement inspection")
	}
	return true, nil
}

func trafficWorkerHasListeners(pid int) (bool, error) {
	listeners := map[string]bool{}
	for _, table := range []string{"tcp", "tcp6", "unix"} {
		file, err := os.Open(fmt.Sprintf("/proc/%d/net/%s", pid, table))
		if err != nil {
			return false, err
		}
		raw, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
		file.Close()
		if err != nil {
			return false, err
		}
		if len(raw) > 8<<20 {
			return false, errors.New("Nginx listener table exceeded its inspection limit")
		}
		for _, line := range strings.Split(string(raw), "\n")[1:] {
			fields := strings.Fields(line)
			if table == "unix" {
				if len(fields) >= 7 {
					flags, err := strconv.ParseUint(fields[3], 16, 32)
					if err != nil {
						return false, err
					}
					if flags&0x10000 != 0 {
						listeners[fields[6]] = true
					}
				}
			} else if len(fields) >= 10 && fields[3] == "0A" {
				listeners[fields[9]] = true
			}
		}
	}
	directory, err := os.Open(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return false, err
	}
	names, err := directory.Readdirnames(8193)
	directory.Close()
	if err != nil && err != io.EOF {
		return false, err
	}
	if len(names) > 8192 {
		return false, errors.New("Nginx listener descriptor inspection exceeded its limit")
	}
	for _, name := range names {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if listeners[inode] {
				return true, nil
			}
		}
	}
	return false, nil
}
