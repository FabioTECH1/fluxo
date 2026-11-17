package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

const httpDefaultsPath = "/etc/nginx/conf.d/fluxo-http-defaults.conf"

// 512 accommodates a complete 253-byte DNS name plus Nginx's hash-entry
// overhead on supported 64-bit targets. This is an HTTP-context default, not
// a site directive or an instruction to replace an operator's explicit value.
const httpDefaultsConfig = "# Managed by Fluxo.\nserver_names_hash_bucket_size 512;\n"

type httpDefaultsEnvironment struct {
	path          string
	trusted       func(string) error
	validate      func(context.Context) error
	loaded        func(context.Context, string) (bool, error)
	syncDirectory func(string) error
	checkpoint    func(string)
}

func ensureHTTPDefaults(ctx context.Context, env httpDefaultsEnvironment) error {
	if err := env.trusted(filepath.Dir(env.path)); err != nil {
		return fmt.Errorf("inspect Nginx HTTP defaults directory: %w", err)
	}
	tx, err := openHTTPDefaultsTransaction(ctx, env)
	if err != nil {
		return err
	}
	defer tx.close()
	if err = tx.recover(ctx); err != nil {
		return err
	}

	if _, err := os.Lstat(env.path); err == nil {
		if err = env.trusted(env.path); err != nil {
			return err
		}
		identity, owned, err := tx.candidate(env.path, nil)
		if err != nil {
			return err
		}
		if !owned {
			return errors.New("preserve custom fluxo-http-defaults.conf; review its HTTP defaults before changing Nginx")
		}
		if err = env.validate(ctx); err != nil {
			// A previous caller may have died after publication but before its
			// duplicate check. Only Nginx's exact authoritative duplicate proof
			// permits withdrawing this exact owned candidate on retry.
			if !explicitHashDuplicate(err, env.path, env.trusted) {
				return err
			}
			if rollbackErr := tx.rollback(ctx, identity); rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return nil
		}
		return requireHTTPDefaultsLoaded(ctx, env)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := env.validate(ctx); err != nil {
		return fmt.Errorf("validate Nginx before HTTP defaults: %w", err)
	}
	identity, published, err := tx.publish()
	if err != nil {
		if published {
			return errors.Join(err, tx.rollback(ctx, identity))
		}
		return err
	}
	if err := env.validate(ctx); err != nil {
		duplicate := explicitHashDuplicate(err, env.path, env.trusted)
		if rollbackErr := tx.rollback(ctx, identity); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		if duplicate {
			return nil
		}
		return fmt.Errorf("Nginx HTTP default validation failed; original configuration restored: %w", err)
	}
	if err := requireHTTPDefaultsLoaded(ctx, env); err != nil {
		return errors.Join(err, tx.rollback(ctx, identity))
	}
	return nil
}

func requireHTTPDefaultsLoaded(ctx context.Context, env httpDefaultsEnvironment) error {
	loaded, err := env.loaded(ctx, env.path)
	if err != nil {
		return err
	}
	if !loaded {
		return errors.New("Nginx does not load the managed HTTP defaults; review its HTTP conf.d include before creating sites")
	}
	return nil
}

func loadedHTTPDefaults(ctx context.Context, path string) (bool, error) {
	out, err := syscmd.Run(ctx, 10*time.Second, nginxBinaryPath, "-T")
	if err != nil {
		// Configuration dumps can contain private operator data. Do not include
		// stdout in diagnostics or persist the dump.
		return false, errors.New("unable to verify that Nginx loads the managed HTTP defaults")
	}
	return strings.Contains(out, "# configuration file "+path+":\n"+httpDefaultsConfig), nil
}

func syncHTTPDefaultsDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func trustedHTTPDefaultsPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && (!info.Mode().IsRegular() || st.Nlink != 1)) {
			return fmt.Errorf("unsafe Nginx configuration path: %s", current)
		}
		if current != path && !info.IsDir() {
			return fmt.Errorf("Nginx configuration ancestor is not a directory: %s", current)
		}
		if current == "/" {
			return nil
		}
	}
}

var hashDuplicateLocation = regexp.MustCompile(`"server_names_hash_bucket_size" directive is duplicate in (/.+):([0-9]+)$`)
var explicitHashLine = regexp.MustCompile(`^\s*server_names_hash_bucket_size\s+[0-9]+\s*;\s*(?:#.*)?$`)

// Nginx decides whether the directive is a duplicate and which declaration
// occurs last. The diagnostic may point at the operator's file when conf.d is
// included before its explicit setting. No configuration parser is invented.
func explicitHashDuplicate(validationErr error, managed string, trusted func(string) error) bool {
	var location []string
	for _, line := range strings.Split(validationErr.Error(), "\n") {
		line = strings.TrimSpace(line)
		match := hashDuplicateLocation.FindStringSubmatch(line)
		if len(match) != 0 {
			if location != nil {
				return false
			}
			location = match
			continue
		}
		if strings.Contains(line, "directive is duplicate") || strings.Contains(line, "[emerg]") || strings.Contains(line, "[alert]") || strings.Contains(line, "[crit]") || strings.Contains(line, "[error]") {
			return false
		}
	}
	if location == nil {
		return false
	}
	path, number := location[1], location[2]
	if filepath.Clean(path) != path || trusted(path) != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 8<<20 {
		return false
	}
	lineNumber, err := strconv.Atoi(number)
	lines := strings.Split(string(raw), "\n")
	if err != nil || lineNumber < 1 || lineNumber > len(lines) || !explicitHashLine.MatchString(lines[lineNumber-1]) {
		return false
	}
	return path != managed || bytes.Equal(raw, []byte(httpDefaultsConfig)) && lineNumber == 2
}
