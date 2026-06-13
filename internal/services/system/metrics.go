package system

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/syscmd"
)

type ServerMetrics struct {
	CPULoad     string `json:"cpu_load"`
	MemTotal    int    `json:"mem_total"` // in MB
	MemUsed     int    `json:"mem_used"`  // in MB
	DiskTotal   string `json:"disk_total"`
	DiskUsed    string `json:"disk_used"`
	DiskUsage   string `json:"disk_usage"`
	DaemonPID   int    `json:"daemon_pid"`
	Platform    string `json:"platform"`
	Port        string `json:"port"`
	HostAddress string `json:"host_address"`
	OSVersion   string `json:"os_version"`
	OSCreated   string `json:"os_created"`
	Hostname    string `json:"hostname"`
}

func getOSVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			version := strings.TrimPrefix(line, "PRETTY_NAME=")
			version = strings.Trim(version, `"'`)
			return version
		}
	}
	return "Linux"
}

func getOSCreatedDate() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try stat birth time of root directory
	out, err := syscmd.Run(ctx, 2*time.Second, "stat", "-c", "%w", "/")
	if err == nil {
		out = strings.TrimSpace(out)
		if out != "" && out != "-" {
			parts := strings.Split(out, " ")
			if len(parts) > 0 {
				t, err := time.Parse("2006-01-02", parts[0])
				if err == nil {
					return t.Format("Jan 02, 2006")
				}
			}
		}
	}

	// Fallback to stat modify time of root directory
	out, err = syscmd.Run(ctx, 2*time.Second, "stat", "-c", "%y", "/")
	if err == nil {
		out = strings.TrimSpace(out)
		if out != "" && out != "-" {
			parts := strings.Split(out, " ")
			if len(parts) > 0 {
				t, err := time.Parse("2006-01-02", parts[0])
				if err == nil {
					return t.Format("Jan 02, 2006")
				}
			}
		}
	}

	return "Unknown"
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func GetMetrics(ctx context.Context) ServerMetrics {
	metrics := ServerMetrics{}

	// System details
	metrics.DaemonPID = os.Getpid()
	metrics.Platform = runtime.GOOS
	metrics.OSVersion = getOSVersion()
	metrics.OSCreated = getOSCreatedDate()

	if h, err := os.Hostname(); err == nil {
		metrics.Hostname = h
	} else {
		metrics.Hostname = "Fluxo Server"
	}

	cfg := config.LoadConfig()
	metrics.Port = cfg.Port
	metrics.HostAddress = getLocalIP()

	// CPU Load
	loadData, err := os.ReadFile("/proc/loadavg")
	if err == nil {
		parts := strings.Fields(string(loadData))
		if len(parts) >= 3 {
			metrics.CPULoad = fmt.Sprintf("%s %s %s", parts[0], parts[1], parts[2])
		}
	}

	// Memory
	memData, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		lines := strings.Split(string(memData), "\n")
		var total, available int
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					val, _ := strconv.Atoi(fields[1])
					total = val / 1024
				}
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					val, _ := strconv.Atoi(fields[1])
					available = val / 1024
				}
			}
		}
		metrics.MemTotal = total
		metrics.MemUsed = total - available
	}

	// Disk
	out, err := syscmd.Run(ctx, 5*time.Second, "df", "-h", "/")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 5 {
				metrics.DiskTotal = fields[1]
				metrics.DiskUsed = fields[2]
				metrics.DiskUsage = fields[4]
			}
		}
	}

	return metrics
}
