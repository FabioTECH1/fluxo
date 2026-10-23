package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errNoConsumerListener = errors.New("Consumer has no listening socket on its managed port")

func listeningInodes(port int) (map[string]bool, error) {
	result := map[string]bool{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			_, hex, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			n, e := strconv.ParseInt(hex, 16, 32)
			if e != nil || int(n) != port {
				continue
			}
			result[fields[9]] = true
		}
	}
	if len(result) == 0 {
		return nil, errNoConsumerListener
	}
	return result, nil
}

// A TCP connection alone might reach another service. Every listener on the
// assigned port must belong exclusively to this verified systemd process group.
func verifyListenerOwnership(ctx context.Context, u unitSnapshot, port int) error {
	inodes, err := listeningInodes(port)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	count := 0
	for _, process := range processes {
		if err = ctx.Err(); err != nil {
			return err
		}
		pid, e := strconv.Atoi(process.Name())
		if e != nil {
			continue
		}
		fds, e := os.ReadDir(filepath.Join("/proc", process.Name(), "fd"))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		for _, fd := range fds {
			count++
			if count > 131072 {
				return errors.New("Socket ownership inspection exceeded its process limit")
			}
			link, e := os.Readlink(filepath.Join("/proc", process.Name(), "fd", fd.Name()))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return e
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if !strings.HasPrefix(link, "socket:[") || !inodes[inode] {
				continue
			}
			identity, e := processKey(pid)
			if e != nil {
				return e
			}
			if u.PIDs[pid] != identity {
				return errors.New("Octane port is also owned by a process outside its managed service")
			}
			owned[inode] = true
		}
	}
	if len(owned) != len(inodes) {
		return errors.New("Unable to prove ownership of every Octane listening socket")
	}
	return nil
}

func octaneHealth(ctx context.Context, branch string, c Consumer, u unitSnapshot) error {
	return octaneHealthWithInspector(ctx, c, u, func(snapshot unitSnapshot) error {
		_, err := inspectOctaneProcesses(ctx, branch, c, snapshot)
		return err
	})
}

func octaneHealthWithInspector(ctx context.Context, c Consumer, u unitSnapshot, inspect func(unitSnapshot) error) error {
	if err := inspect(u); err != nil {
		return err
	}
	if err := octaneHTTP(ctx, c.Port); err != nil {
		return err
	}
	current, err := observeUnit(ctx, u.Name, u.CGroup)
	if err != nil {
		return err
	}
	if !sameOriginalMain(u, current) {
		return errors.New("Octane controller changed during HTTP verification")
	}
	return inspect(current)
}

func verifyOptionalConsumerListener(ctx context.Context, u unitSnapshot, port int) error {
	err := verifyListenerOwnership(ctx, u, port)
	if errors.Is(err, errNoConsumerListener) {
		return nil
	}
	return err
}

func octaneHTTP(ctx context.Context, port int) error {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode >= 500 {
		return fmt.Errorf("Octane returned HTTP %d", response.StatusCode)
	}
	return nil
}
