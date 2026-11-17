package mysql

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const mysql8FixturePrefix = "/var/lib/fluxo-mysql8-acceptance-"

// Exact signed-index/extraction evidence supplied by the parent provisioning pass.
const mysql8ArchiveManifestSHA256 = "9048ba64d0e1d18c94c0fad1d13344518de10834d897d4c07ca17b4d86d58ca9"
const mysql8PayloadManifestSHA256 = "beb8c689638ef0a2a4d1de0ee67f39aacc95cb0823ac479ee277c934c60d7f74"

func mysql8VMGuard() bool {
	host, _ := os.Hostname()
	return os.Geteuid() == 0 && host == "fluxo-bridge-prod-20261101" && os.Getenv("FLUXO_MYSQL8_INTEGRATION") == "1"
}

// TestVMMySQL8PartialRevokes uses a new mount+network namespace. It never stops,
// replaces, installs into, or authenticates to the host MariaDB from the child.
// Extracted inputs and every private database fixture remain for investigation.
func TestVMMySQL8PartialRevokes(t *testing.T) {
	if !mysql8VMGuard() {
		t.Skip("root explicit opt-in on own Noble acceptance VM only")
	}
	if os.Getenv("FLUXO_MYSQL8_CHILD") == "1" {
		mysql8RunPrivate(t)
		return
	}
	image := os.Getenv("FLUXO_MYSQL8_IMAGE")
	if !filepath.IsAbs(image) || filepath.Clean(image) != image {
		t.Fatal("canonical private MySQL8 input directory required")
	}
	if _, err := mysql8VerifyImage(image); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := mysql8FixturePrefix + uuid.NewString()
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	parentMount, _ := os.Readlink("/proc/self/ns/mnt")
	parentNet, _ := os.Readlink("/proc/self/ns/net")
	before, err := mysql8HostIdentity()
	if err != nil {
		t.Fatal("host baseline unavailable")
	}
	// Registered before the child starts: setup failures must not bypass proof.
	t.Cleanup(func() {
		after, e := mysql8HostIdentity()
		if e != nil || !bytes.Equal(before, after) {
			t.Error("host MariaDB identity/catalog changed during isolated test")
		}
	})
	log, err := os.OpenFile(filepath.Join(root, "test.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run", "^TestVMMySQL8PartialRevokes$", "-test.v", "-test.timeout=3m")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "FLUXO_MYSQL8_INTEGRATION=1", "FLUXO_MYSQL8_CHILD=1", "FLUXO_MYSQL8_IMAGE=" + image, "FLUXO_MYSQL8_ROOT=" + root, "FLUXO_MYSQL8_PARENT_MNT=" + parentMount, "FLUXO_MYSQL8_PARENT_NET=" + parentNet}
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS | unix.CLONE_NEWNET, Pdeathsig: syscall.SIGTERM}
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Run(); err != nil {
		t.Fatalf("private MySQL8 acceptance failed; retained evidence %s", root)
	}
	t.Logf("private MySQL8 ON/OFF acceptance passed; retained evidence %s", root)
}

func mysql8HostIdentity() ([]byte, error) {
	db, err := openAdmin()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var version, dir, host string
	if err = db.QueryRowContext(ctx, "SELECT @@version,@@datadir,@@hostname").Scan(&version, &dir, &host); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	socket, err := os.Stat("/run/mysqld/mysqld.sock")
	if err != nil {
		return nil, err
	}
	st, ok := socket.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("socket identity unavailable")
	}
	return json.Marshal([]any{version, dir, host, names, st.Dev, st.Ino})
}

func mysql8Protected(path string) ([]byte, error) {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("untrusted MySQL8 input ancestry")
		}
		if current == path {
			if !info.Mode().IsRegular() || st.Nlink != 1 || info.Size() > 128<<20 {
				return nil, errors.New("unsafe MySQL8 input payload")
			}
		} else if !info.IsDir() {
			return nil, errors.New("invalid MySQL8 input parent")
		}
		if current == "/" {
			break
		}
	}
	return os.ReadFile(path)
}

// manifest.sha256 is a protected checksum manifest produced by the signed
// Ubuntu archive extraction step. Trust in signed-index verification is external
// test provisioning evidence, not inferred from a self-authored checksum file.
func mysql8VerifyImage(root string) (map[string]string, error) {
	raw, err := mysql8Protected(filepath.Join(root, "manifest.sha256"))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != mysql8PayloadManifestSHA256 {
		return nil, errors.New("unreviewed MySQL8 payload manifest")
	}
	archives, err := mysql8Protected(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, err
	}
	archiveSum := sha256.Sum256(archives)
	if hex.EncodeToString(archiveSum[:]) != mysql8ArchiveManifestSHA256 {
		return nil, errors.New("unreviewed MySQL8 archive attestation")
	}
	inputs := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 67 || line[64:66] != "  " {
			return nil, errors.New("invalid MySQL8 input manifest")
		}
		rel := line[66:]
		sum := line[:64]
		if filepath.IsAbs(rel) || filepath.Clean(rel) != rel || strings.HasPrefix(rel, "../") || !strings.HasPrefix(rel, "image/") {
			return nil, errors.New("noncanonical MySQL8 input path")
		}
		if _, e := hex.DecodeString(sum); e != nil {
			return nil, errors.New("invalid MySQL8 input digest")
		}
		path := filepath.Join(root, rel)
		if _, exists := inputs[path]; exists {
			return nil, errors.New("duplicate MySQL8 input manifest entry")
		}
		data, e := mysql8Protected(path)
		if e != nil {
			return nil, e
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != sum {
			return nil, errors.New("MySQL8 extracted input changed")
		}
		inputs[path] = sum
	}
	mysqld := filepath.Join(root, "image/usr/sbin/mysqld")
	if _, ok := inputs[mysqld]; !ok {
		return nil, errors.New("MySQL8 server payload absent from manifest")
	}
	if err = mysql8ELFClosure(mysqld, root, inputs); err != nil {
		return nil, err
	}
	return inputs, nil
}

func mysql8ELFClosure(binary, root string, inputs map[string]string) error {
	// Read protected installed metadata without executing dpkg-query or a loader.
	md5s := map[string]map[string]bool{}
	manifests, err := filepath.Glob("/var/lib/dpkg/info/*.md5sums")
	if err != nil {
		return err
	}
	for _, path := range manifests {
		raw, e := mysql8Protected(path)
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if len(line) < 35 || line[32:34] != "  " {
				continue
			}
			rel := line[34:]
			if filepath.Clean(rel) != rel || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") {
				return errors.New("noncanonical installed package payload")
			}
			if strings.HasPrefix(rel, "usr/lib/") || strings.HasPrefix(rel, "lib/") || strings.HasPrefix(rel, "lib64/") {
				path := filepath.Join("/", rel)
				for _, alias := range []string{path} {
					if md5s[alias] == nil {
						md5s[alias] = map[string]bool{}
					}
					md5s[alias][line[:32]] = true
				}
				if real, e := filepath.EvalSymlinks(path); e == nil {
					if md5s[real] == nil {
						md5s[real] = map[string]bool{}
					}
					md5s[real][line[:32]] = true
				}
			}
		}
	}
	visited := map[string]bool{}
	var walk func(string) error
	walk = func(path string) error {
		real, e := filepath.EvalSymlinks(path)
		if e != nil {
			return e
		}
		if strings.HasPrefix(path, filepath.Join(root, "image")+string(os.PathSeparator)) && !strings.HasPrefix(real, filepath.Join(root, "image")+string(os.PathSeparator)) {
			return errors.New("private MySQL8 dependency alias escaped image")
		}
		if visited[real] {
			return nil
		}
		visited[real] = true
		raw, e := mysql8Protected(real)
		if e != nil {
			return e
		}
		if want, ok := inputs[real]; ok {
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != want {
				return errors.New("private MySQL8 dependency changed")
			}
		} else {
			sum := md5.Sum(raw)
			got := hex.EncodeToString(sum[:])
			if !md5s[path][got] && !md5s[real][got] {
				return errors.New("MySQL8 host dependency lacks matching protected vendor metadata")
			}
		}
		object, e := elf.NewFile(bytes.NewReader(raw))
		if e != nil {
			return errors.New("MySQL8 dependency is not ELF")
		}
		defer object.Close()
		if interp := object.Section(".interp"); interp != nil {
			data, e := interp.Data()
			if e != nil {
				return e
			}
			if e = walk(strings.TrimRight(string(data), "\x00")); e != nil {
				return e
			}
		}
		libs, e := object.ImportedLibraries()
		if e != nil {
			return e
		}
		for _, name := range libs {
			if name != filepath.Base(name) {
				return errors.New("nonstandard MySQL8 dependency selector")
			}
			found := ""
			for _, dir := range []string{filepath.Join(root, "image/usr/lib/x86_64-linux-gnu"), filepath.Join(root, "image/lib/x86_64-linux-gnu"), "/usr/lib/x86_64-linux-gnu", "/lib/x86_64-linux-gnu", "/usr/lib", "/lib"} {
				candidate := filepath.Join(dir, name)
				if _, e := os.Stat(candidate); e == nil {
					found = candidate
					break
				}
			}
			if found == "" {
				return fmt.Errorf("private MySQL8 dependency missing: %s", name)
			}
			if e = walk(found); e != nil {
				return e
			}
		}
		return nil
	}
	return walk(binary)
}

func mysql8RunPrivate(t *testing.T) {
	root := os.Getenv("FLUXO_MYSQL8_ROOT")
	image := os.Getenv("FLUXO_MYSQL8_IMAGE")
	if !strings.HasPrefix(root, mysql8FixturePrefix) || filepath.Clean(root) != root {
		t.Fatal("invalid private MySQL8 fixture")
	}
	mount, _ := os.Readlink("/proc/self/ns/mnt")
	net, _ := os.Readlink("/proc/self/ns/net")
	if mount == os.Getenv("FLUXO_MYSQL8_PARENT_MNT") || net == os.Getenv("FLUXO_MYSQL8_PARENT_NET") {
		t.Fatal("private namespaces not established")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	request, err := unix.NewIfreq("lo")
	if err == nil {
		err = unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request)
	}
	if err == nil {
		request.SetUint16(request.Uint16() | unix.IFF_UP)
		err = unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request)
	}
	unix.Close(fd)
	if err != nil {
		t.Fatal("private loopback setup failed")
	}
	for _, name := range []string{"data", "runtime", "files", "tmp"} {
		if err = os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = unix.Mount(filepath.Join(root, "runtime"), "/run/mysqld", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = mysql8VerifyImage(image); err != nil {
		t.Fatal(err)
	}
	mysqld := filepath.Join(image, "image/usr/sbin/mysqld")
	env := []string{"LD_LIBRARY_PATH=" + filepath.Join(image, "image/usr/lib/x86_64-linux-gnu"), "TMPDIR=" + filepath.Join(root, "tmp")}
	base := []string{"--no-defaults", "--user=root", "--basedir=" + filepath.Join(image, "image/usr"), "--datadir=" + filepath.Join(root, "data"), "--plugin-dir=" + filepath.Join(image, "image/usr/lib/mysql/plugin"), "--lc-messages-dir=" + filepath.Join(image, "image/usr/share/mysql"), "--secure-file-priv=" + filepath.Join(root, "files"), "--tmpdir=" + filepath.Join(root, "tmp"), "--socket=/run/mysqld/mysqld.sock", "--pid-file=" + filepath.Join(root, "mysqld.pid")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var version bytes.Buffer
	if err = syscmd.RunMaintenance(ctx, "", &version, env, mysqld, "--no-defaults", "--version"); err != nil || !strings.Contains(version.String(), "Ver 8.0.46") {
		t.Fatal("exact private MySQL8 build required")
	}
	initLog, err := os.OpenFile(filepath.Join(root, "initialize.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = syscmd.RunMaintenance(ctx, "", initLog, env, mysqld, append(append([]string{}, base...), "--initialize-insecure")...)
	initLog.Close()
	if err != nil {
		t.Fatal("private MySQL8 initialization failed; see retained log")
	}
	serverLog, err := os.OpenFile(filepath.Join(root, "server.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()
	args := append(append([]string{}, base...), "--socket=/run/mysqld/mysqld.sock", "--port=3306", "--bind-address=127.0.0.1", "--mysqlx=OFF", "--skip-name-resolve", "--pid-file="+filepath.Join(root, "mysqld.pid"), "--log-error="+filepath.Join(root, "error.log"), "--secure-file-priv="+filepath.Join(root, "files"), "--local-infile=OFF", "--tmpdir="+filepath.Join(root, "tmp"), "--innodb-buffer-pool-size=32M", "--max-connections=12", "--skip-log-bin")
	cmd := exec.Command(mysqld, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	cmd.Env = append([]string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}, env...)
	cmd.Stdout = serverLog
	cmd.Stderr = serverLog
	if err = cmd.Start(); err != nil {
		t.Fatal("private MySQL8 start failed")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	var admin *sql.DB
	for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); {
		admin, err = openAdmin()
		if err == nil {
			err = admin.PingContext(ctx)
			if err == nil {
				break
			}
			admin.Close()
		}
		select {
		case <-done:
			t.Fatal("private MySQL8 stopped during startup")
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || admin == nil {
		t.Fatal("private MySQL8 socket unavailable")
	}
	defer admin.Close()
	var dataDir, actualVersion string
	if err = admin.QueryRow("SELECT @@datadir,@@version").Scan(&dataDir, &actualVersion); err != nil || strings.TrimSuffix(dataDir, "/") != filepath.Join(root, "data") || !strings.HasPrefix(actualVersion, "8.0.46") {
		t.Fatal("private server identity disagrees")
	}
	results := []any{}
	for _, mode := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial-revokes-%v", mode), func(t *testing.T) { results = append(results, mysql8GrantCase(t, admin, mode)) })
	}
	evidence, _ := json.MarshalIndent(map[string]any{"version": actualVersion, "private_mount_namespace": true, "private_network_namespace": true, "host_packages_changed": false, "cases": results}, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "evidence.json"), append(evidence, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func mysql8GrantCase(t *testing.T, admin *sql.DB, literal bool) any {
	t.Helper()
	mode := "OFF"
	if literal {
		mode = "ON"
	}
	if _, err := admin.Exec("SET GLOBAL partial_revokes=" + mode); err != nil {
		t.Fatal("cannot select private grant mode")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "fx_m8_" + hex.EncodeToString(random[:5])
	user := name
	shadow := strings.ReplaceAll(name, "_", "x")
	password := hex.EncodeToString(random[:]) + "-fixture"
	if err := CreateDatabase(name, user, password); err != nil {
		t.Fatal("private database creation failed")
	}
	if err := CreateDatabaseOnly(shadow); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("native fresh grant catalog refused", err)
	}
	if err := VerifyDatabaseAccess(name, user, password); err != nil {
		t.Fatal("native fresh password access failed")
	}
	account := "'" + user + "'@'" + LocalTCPHost + "'"
	var before string
	if err := admin.QueryRow("SELECT authentication_string FROM mysql.user WHERE User=? AND Host=?", user, LocalTCPHost).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account); err != nil {
		t.Fatal(err)
	}
	bad := grantDatabasePatternForMode(name, !literal)
	// Seed ON's legacy escaped grant under its original OFF interpretation,
	// then reproduce the platform mode transition before repair.
	if literal {
		if _, err := admin.Exec("SET GLOBAL partial_revokes=OFF"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec("GRANT ALL PRIVILEGES ON `" + bad + "`.* TO " + account); err != nil {
		t.Fatal("legacy grant seed failed")
	}
	if literal {
		if _, err := admin.Exec("SET GLOBAL partial_revokes=ON"); err != nil {
			t.Fatal("historical partial-revokes transition refused", err)
		}
	}
	if BridgeDatabaseReady(ctx, name, user, false) == nil {
		t.Fatal("native malformed/wildcard catalog admitted")
	}
	if _, err := admin.Exec("SET GLOBAL general_log=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("SET GLOBAL log_output='TABLE'"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("TRUNCATE TABLE mysql.general_log"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("SET GLOBAL general_log=ON"); err != nil {
		t.Fatal(err)
	}
	repairErr := ReplaceDatabaseAccess(user, []string{name})
	if _, err := admin.Exec("SET GLOBAL general_log=OFF"); err != nil {
		t.Fatal(err)
	}
	if repairErr != nil {
		t.Fatal("native selected-scope repair failed", repairErr)
	}
	rows, err := admin.Query("SELECT argument FROM mysql.general_log WHERE command_type='Query' AND (argument LIKE 'GRANT %' OR argument LIKE 'REVOKE %') ORDER BY event_time")
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{}
	for rows.Next() {
		var query string
		if err = rows.Scan(&query); err != nil {
			t.Fatal(err)
		}
		ordered = append(ordered, query)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		t.Fatal(rowErr)
	}
	if len(ordered) != 2 || !strings.HasPrefix(ordered[0], "GRANT ALL PRIVILEGES ON ") || !strings.HasPrefix(ordered[1], "REVOKE ALL PRIVILEGES ON ") {
		t.Fatal("native repair did not add before deleting")
	}
	var after string
	if err = admin.QueryRow("SELECT authentication_string FROM mysql.user WHERE User=? AND Host=?", user, LocalTCPHost).Scan(&after); err != nil || before != after {
		t.Fatal("repair changed application authentication material")
	}
	if err = VerifyDatabaseAccess(name, user, password); err != nil {
		t.Fatal("repair changed effective application password")
	}
	if err = BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("native repaired catalog refused", err)
	}
	rows, err = admin.Query("SHOW GRANTS FOR " + account)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := readStatementRows(rows)
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	current := databaseGrants(grants)
	if len(grants) != 2 || len(current) != 1 {
		t.Fatal("unexpected native repaired authority")
	}
	if _, ok := current[grantDatabasePatternForMode(name, literal)]; !ok {
		t.Fatal("repaired SHOW GRANTS is not the exact literal scope")
	}
	cfg := driver.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = LocalTCPHost + ":3306"
	cfg.DBName = name
	app, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err = app.ExecContext(ctx, "CREATE TABLE `"+name+"`.allowed_fixture(id INTEGER)"); err != nil {
		t.Fatal("literal own database grant ineffective")
	}
	if _, err = app.ExecContext(ctx, "CREATE TABLE `"+shadow+"`.forbidden_fixture(id INTEGER)"); err == nil {
		t.Fatal("underscore grant leaked to shadow schema")
	}
	return map[string]any{"partial_revokes": mode, "database": name, "literal_scope_exact": true, "bridge_catalog_pass": true, "password_preserved": true, "addition_preceded_removal": true, "shadow_access_refused": true}
}
