package maintenancetask

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

type installedInput struct{ name, version, dependencies, provides string }

func dependencyName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " (\t"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

// inputClosure starts from fixed profile packages and includes their installed
// interpreter/library closure. It reads metadata, not maintainer scripts or
// package tools' administrative hook configurations.
func inputClosure(ctx context.Context, roots []string) ([]installedInput, error) {
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	err := syscmd.RunMaintenance(probe, "", &output, nil, "/usr/bin/dpkg-query", "--admindir=/var/lib/dpkg", "-W", "-f=${binary:Package}\t${db:Status-Status}\t${Version}\t${Depends}\t${Pre-Depends}\t${Provides}\n")
	if err != nil {
		return nil, err
	}
	installed := map[string]installedInput{}
	providers := map[string][]string{}
	for _, line := range strings.Split(output.String(), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, errors.New("Incomplete installed dependency metadata")
		}
		if f[1] != "installed" {
			continue
		}
		if parts := strings.SplitN(f[0], ":", 2); len(parts) == 2 && parts[1] != runtime.GOARCH {
			continue
		}
		name := dependencyName(f[0])
		record := installedInput{f[0], f[2], f[3] + "," + f[4], f[5]}
		installed[name] = record
		for _, provided := range strings.Split(f[5], ",") {
			if key := dependencyName(provided); key != "" {
				providers[key] = append(providers[key], name)
			}
		}
	}
	queue := append([]string(nil), roots...)
	seen := map[string]bool{}
	var result []installedInput
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		record, ok := installed[name]
		if !ok {
			return nil, errors.New("A required scheduled-job distribution dependency is not installed: " + name)
		}
		seen[name] = true
		result = append(result, record)
		for _, group := range strings.Split(record.dependencies, ",") {
			if strings.TrimSpace(group) == "" {
				continue
			}
			found := false
			for _, alternative := range strings.Split(group, "|") {
				dependency := dependencyName(alternative)
				if _, ok := installed[dependency]; ok {
					queue = append(queue, dependency)
					found = true
					break
				}
				if choices := providers[dependency]; len(choices) > 0 {
					queue = append(queue, choices[0])
					found = true
					break
				}
			}
			if !found {
				return nil, errors.New("A scheduled-job dependency alternative is unavailable: " + group)
			}
		}
	}
	return result, nil
}
