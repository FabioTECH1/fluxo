package scheduler

// Read-only PAM/NSS/ELF closure for the reviewed loader profiles. Inspection
// grants no launch, publication or migration authority on its own.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type pamCandidateReport struct {
	Candidate         string              `json:"candidate"`
	Revision          string              `json:"revision"`
	Qualified         bool                `json:"qualified"`
	ProductionEnabled bool                `json:"production_enabled"`
	Files             map[string]string   `json:"files"`
	Packages          map[string]string   `json:"packages"`
	Dependencies      map[string][]string `json:"elf_dependencies"`
	Identity          map[string]string   `json:"identity"`
	Failure           string              `json:"failure,omitempty"`
	TrustBoundary     string              `json:"trust_boundary"`
}

type pamCandidateInspection struct {
	report      pamCandidateReport
	observed    map[string]pamCandidateObservation
	payload     map[string]pamCandidatePayload
	cache       map[string][]string
	search      []string
	resolutions map[string]string
}

func inspectPAMCandidate(id string, pid int) (pamCandidateReport, error) {
	return retryPAMDirectorySnapshot(func() (pamCandidateReport, error) { return inspectPAMCandidateOnce(id, pid) })
}

// A transient unit being created/collected can change an otherwise protected
// directory during inspection. Discard that entire observation and start a
// fresh complete qualification. Never ignore a changed file, owner, mode,
// inode, alias, selector or a failed qualification; all three attempts must
// still end with the ordinary exact revalidation before returning success.
func retryPAMDirectorySnapshot(inspect func() (pamCandidateReport, error)) (pamCandidateReport, error) {
	var report pamCandidateReport
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		report, err = inspect()
		var churn *pamDirectorySnapshotChanged
		if !errors.As(err, &churn) {
			return report, err
		}
	}
	return report, err
}

func inspectPAMCandidateOnce(id string, pid int) (pamCandidateReport, error) {
	p := &pamCandidateInspection{report: pamCandidateReport{Candidate: id, Revision: "disabled-cron-pam-nss-v1", Files: map[string]string{}, Packages: map[string]string{}, Dependencies: map[string][]string{}, Identity: map[string]string{}, TrustBoundary: "Protected local dpkg vendor payload metadata and authentic reviewed Ubuntu PAM/libcap/systemd; vendor libc/dependencies are a finite platform trust boundary, not a whole-OS proof."}, observed: map[string]pamCandidateObservation{}, payload: map[string]pamCandidatePayload{}}
	if id == cronieLoaderPAMProfileID {
		p.report.Revision = "noble-cronie-pam-nss-v1"
	}
	err := p.inspect(id, pid)
	if err != nil {
		p.report.Failure = err.Error()
	} else {
		p.report.Qualified = true
	}
	return p.report, err
}

func (p *pamCandidateInspection) inspect(id string, pid int) error {
	if os.Geteuid() != 0 || runtime.GOARCH != "amd64" {
		return errors.New("PAM loader profile requires root on reviewed amd64 Ubuntu")
	}
	c, err := distributionCandidate(id)
	if err != nil {
		return err
	}
	if id == cronieLoaderPAMProfileID {
		release, e := p.read("/usr/lib/os-release", 1<<20)
		if e != nil {
			return e
		}
		if e = validateCronieLoaderRelease(release); e != nil {
			return e
		}
	}
	// Retain the existing immutable active-stack/config pins. This inspector does
	// not qualify the daemon itself: native distribution tests do that separately.
	for path, want := range c.files {
		raw, e := p.read(path, 4<<20)
		if e != nil {
			return e
		}
		if hashBytes(raw) != want {
			return fmt.Errorf("candidate pinned configuration differs: %s", path)
		}
	}

	for directory, expected := range c.directories {
		if _, e := p.canonical(directory); os.IsNotExist(e) && len(expected) == 0 {
			p.report.Files[directory] = "absent"
			continue
		} else if e != nil {
			return e
		}
		entries, e := os.ReadDir(directory)
		if e != nil {
			return e
		}
		if len(entries) != len(expected) {
			return fmt.Errorf("unreviewed PAM fragment directory: %s", directory)
		}
		for i, entry := range entries {
			if entry.Name() != expected[i] || !entry.Type().IsRegular() {
				return fmt.Errorf("unreviewed PAM fragment: %s", directory)
			}
		}
	}
	if c.localeTarget != "/etc/default/locale" {
		if err = candidateProtectedAlias("/etc/default/locale", c.localeTarget); err != nil {
			return err
		}
	}
	if _, err = p.read("/etc/default/locale", 1<<20); err != nil {
		return err
	}
	if err = p.configurations(c.release); err != nil {
		return err
	}
	if err = p.vendorPayloads(id); err != nil {
		return err
	}
	if err = p.loaderConfiguration(); err != nil {
		return err
	}
	if err = p.userdbServiceConfiguration(id); err != nil {
		return err
	}
	roots := []string{c.executable, "/usr/sbin/unix_chkpwd", "/usr/lib/x86_64-linux-gnu/libnss_systemd.so.2"}
	for _, name := range c.pamModules {
		roots = append(roots, "/usr/lib/x86_64-linux-gnu/security/"+name)
	}

	actualNSS, e := p.resolveLibrary("libnss_systemd.so.2", nil)
	if e != nil {
		return e
	}
	expectedNSS, e := p.canonical("/usr/lib/x86_64-linux-gnu/libnss_systemd.so.2")
	if e != nil || actualNSS != expectedNSS {
		return errors.New("NSS systemd loader resolution differs from reviewed module")
	}
	// glibc >=2.34 implements files NSS in libc. Any compatibility DSO selected
	// by the cache is still authenticated if present; custom shadowing is refused.
	if _, exists := p.cache["libnss_files.so.2"]; exists {
		path, e := p.resolveLibrary("libnss_files.so.2", nil)
		if e != nil {
			return e
		}
		roots = append(roots, path)
	}
	for _, path := range roots {
		if err = p.elfClosure(path, map[string]bool{}); err != nil {
			return err
		}
	}
	if err = p.accounts(); err != nil {
		return err
	}
	// Prove the absence of alternate credential files' arbitrary writes without
	// reading, hashing or disclosing their credential contents.
	for _, path := range []string{"/etc/shadow", "/etc/gshadow"} {
		if err = p.sensitiveMetadata(path); err != nil {
			return err
		}
	}
	if err = p.daemonLoaderInputs(pid, c.binary); err != nil {
		return err
	}
	if id == nobleLoaderPAMProfileID || id == cronieLoaderPAMProfileID {
		if err = p.nobleSELinuxInputs(pid); err != nil {
			return err
		}
	}
	for name, want := range p.resolutions {
		got, e := p.resolveLibrary(name, nil)
		if e != nil {
			return e
		}
		if got != want {
			return errors.New("transitive loader resolution changed during closure")
		}
	}
	return p.revalidate()
}

func (p *pamCandidateInspection) configurations(release string) error {
	// All cron include targets are already pinned above. libpam also initializes
	// the 'other' stack; only its stock include closure and known vendor modules
	// are permitted, including never-dispatched password/session fallback stacks.
	for _, path := range []string{"/etc/pam.d/other", "/etc/pam.d/common-password", "/etc/pam.d/common-session"} {
		raw, err := p.read(path, 1<<20)
		if err != nil {
			return err
		}
		if err = validatePAMCandidateFallback(path, raw); err != nil {
			return err
		}
	}

	raw, err := p.optional("/etc/pam.conf", 1<<20)
	if err != nil {
		return err
	}
	if !pamCandidateCommentsOnly(raw) {
		return errors.New("candidate refuses active legacy pam.conf directives")
	}
	raw, err = p.optional("/etc/security/capability.conf", 1<<20)
	if err != nil {
		return err
	}
	if !pamCandidateCapabilityPolicy(raw) {
		return errors.New("candidate accepts only inert or exact none-wildcard capability policy")
	}

	// pam_umask uses these names (not /etc/login.conf). Values select umask and
	// group-bit policy, not executable hooks. Bind whole protected contents.
	for _, path := range []string{"/etc/login.defs", "/etc/default/login"} {
		if _, err := p.optional(path, 1<<20); err != nil {
			return err
		}
	}

	for _, path := range []string{"/run/nscd/socket", "/var/run/nscd/socket"} {
		if _, e := p.canonical(path); !os.IsNotExist(e) {
			return fmt.Errorf("unreviewed external NSS cache authority: %s", path)
		}
		p.report.Files[path] = "absent"
	}
	raw, err = p.read("/etc/nsswitch.conf", 1<<20)
	if err != nil {
		return err
	}
	if err = validatePAMCandidateNSSForRelease(raw, release); err != nil {
		return err
	}
	return p.userdbConfiguration()
}

// Both reviewed pam_cap versions treat 'none' as clearing inheritable and
// ambient sets, without adding bounding/effective/permitted capabilities.
func pamCandidateCapabilityPolicy(raw []byte) bool {
	// pam_cap uses fgets(buffer, 4096), so a long physical comment can
	// become a separate active record on its next read. Keep each entire
	// physical line, including its newline, inside one vendor record and use
	// only the ASCII whitespace understood by that parser.
	for _, line := range strings.Split(string(raw), "\n") {
		if len(line) > 4094 {
			return false
		}
		for _, b := range []byte(line) {
			if b < 0x20 && b != '\t' || b > 0x7e {
				return false
			}
		}
	}
	active := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if active || len(fields) < 2 || fields[0] != "none" || fields[1] != "*" || len(fields) > 2 && !strings.HasPrefix(fields[2], "#") {
			return false
		}
		active = true
	}
	return true
}

func pamCandidateCommentsOnly(raw []byte) bool {
	// These files are consumed by C parsers, including the ELF loader.
	// Unicode whitespace must never turn a possible filename into absence.
	for _, b := range raw {
		if b > 0x7e || b < 0x20 && b != '\t' && b != '\n' {
			return false
		}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.Trim(strings.SplitN(line, "#", 2)[0], " \t")
		if line != "" {
			return false
		}
	}
	return true
}

var pamCandidateLine = regexp.MustCompile(`^(auth|account|password|session)\s+(\[[^\]]+\]|\S+)\s+(\S+)(?:\s+(.*))?$`)

func validatePAMCandidateFallback(path string, raw []byte) error {
	allowedIncludes := map[string]bool{"common-auth": true, "common-account": true, "common-password": true, "common-session": true}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "@include ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "@include "))
			if filepath.Base(path) != "other" || !allowedIncludes[name] || seen[name] {
				return fmt.Errorf("unreviewed PAM include in %s", path)
			}
			seen[name] = true
			continue
		}
		m := pamCandidateLine.FindStringSubmatch(line)
		if m == nil || filepath.Base(path) == "other" {
			return fmt.Errorf("unreviewed PAM fallback syntax in %s", path)
		}

		expectedType := "session"
		if filepath.Base(path) == "common-password" {
			expectedType = "password"
		}
		if m[1] != expectedType {
			return fmt.Errorf("unreviewed PAM fallback management group in %s", path)
		}
		args := strings.Join(strings.Fields(m[4]), " ")
		control := strings.Join(strings.Fields(m[2]), " ")
		valid := false
		switch m[3] {
		case "pam_deny.so":
			valid = args == "" && control == "requisite"
		case "pam_permit.so":
			valid = args == "" && (control == "required" || m[1] == "session" && control == "[default=1]")
		case "pam_unix.so":
			valid = m[1] == "session" && args == "" && control == "required" || m[1] == "password" && args == "obscure yescrypt" && control == "[success=1 default=ignore]"
		case "pam_umask.so", "pam_systemd.so":
			valid = m[1] == "session" && args == "" && control == "optional"
		}
		if !valid {
			return fmt.Errorf("unreviewed PAM fallback module/options in %s", path)
		}
	}
	if filepath.Base(path) == "other" && len(seen) != len(allowedIncludes) {
		return errors.New("PAM other fallback include closure is incomplete")
	}
	return nil
}

func validatePAMCandidateNSS(raw []byte) error {
	return validatePAMCandidateNSSForRelease(raw, "22.04")
}
func validatePAMCandidateNSSForRelease(raw []byte, release string) error {
	want := map[string]string{"passwd": "files systemd", "group": "files systemd", "shadow": "files", "gshadow": "files"}
	if release == "26.04" || release == "24.04" {
		want["shadow"], want["gshadow"] = "files systemd", "files systemd"
	} else if release != "22.04" {
		return errors.New("unknown NSS candidate release")
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return errors.New("invalid protected NSS policy")
		}
		name = strings.TrimSpace(name)
		value = strings.Join(strings.Fields(value), " ")
		if name == "initgroups" {
			if seen[name] {
				return errors.New("duplicate NSS initgroups policy")
			}
			seen[name] = true
			if value != "files systemd" {
				return errors.New("unreviewed NSS initgroups provider")
			}
			continue
		}
		if expected, active := want[name]; active {
			if seen[name] || value != expected {
				return fmt.Errorf("unreviewed NSS %s provider/control chain", name)
			}
			seen[name] = true
		}
	}
	for name := range want {
		if !seen[name] {
			return fmt.Errorf("missing NSS %s policy", name)
		}
	}
	return nil
}

func (p *pamCandidateInspection) accounts() error {
	passwd, err := p.readPrivateIdentity("/etc/passwd")
	if err != nil {
		return err
	}
	group, err := p.readPrivateIdentity("/etc/group")
	if err != nil {
		return err
	}
	identities, err := validatePAMCandidateAccounts(passwd, group)
	if err != nil {
		return err
	}
	if p.report.Identity == nil {
		p.report.Identity = make(map[string]string)
	}
	for name, identity := range identities {
		p.report.Identity[name] = identity
	}
	return nil
}

func validatePAMCandidateAccounts(passwd, group []byte) (map[string]string, error) {
	result := map[string]string{}
	ids := map[string]int{}
	gids := map[string]int{}
	allIDs := map[int]string{}
	for _, line := range strings.Split(string(passwd), "\n") {
		if line == "" {
			continue
		}
		v := strings.Split(line, ":")
		if len(v) != 7 {
			return nil, errors.New("invalid protected local identity format")
		}
		uid, e := strconv.Atoi(v[2])
		gid, e2 := strconv.Atoi(v[3])
		if e != nil || e2 != nil || uid < 0 || gid < 0 {
			return nil, errors.New("invalid protected local identity number")
		}
		if old, ok := allIDs[uid]; ok && (uid == 0 || v[0] == "fluxo" || old == "fluxo") {
			return nil, errors.New("ambiguous protected execution identity")
		}
		allIDs[uid] = v[0]
		if v[0] != "root" && v[0] != "fluxo" {
			continue
		}
		if _, ok := ids[v[0]]; ok {
			return nil, errors.New("duplicate protected execution account")
		}
		ids[v[0]], gids[v[0]] = uid, gid
		if v[1] != "x" && v[1] != "*" && v[1] != "!" {
			return nil, errors.New("execution identity credentials must use protected shadow semantics")
		}
		for _, field := range strings.Split(v[4], ",") {
			field = strings.ToLower(strings.TrimSpace(field))
			for _, prefix := range []string{"umask=", "pri=", "ulimit="} {
				if strings.HasPrefix(field, prefix) {
					return nil, errors.New("execution identity has an unreviewed PAM GECOS override")
				}
			}
		}
		if v[0] == "root" {
			if uid != 0 || gid != 0 || v[5] != "/root" || v[6] != "/bin/bash" {
				return nil, errors.New("root execution identity differs from reviewed semantics")
			}
		} else if uid < 1000 || gid < 1000 || v[5] != "/home/fluxo" || v[6] != "/bin/bash" {
			return nil, errors.New("Fluxo execution identity differs from reviewed semantics")
		}
		result[v[0]] = strconv.Itoa(uid) + ":" + strconv.Itoa(gid) + ":" + v[5] + ":" + v[6]
	}
	if len(ids) != 2 {
		return nil, errors.New("candidate requires existing root and Fluxo local identities")
	}
	groups := []string{}
	primary := false
	for _, line := range strings.Split(string(group), "\n") {
		if line == "" {
			continue
		}
		v := strings.Split(line, ":")
		if len(v) != 4 {
			return nil, errors.New("invalid protected local group format")
		}
		gid, e := strconv.Atoi(v[2])
		if e != nil || gid < 0 {
			return nil, errors.New("invalid protected local group number")
		}
		if v[0] == "fluxo" {
			if primary || gid != gids["fluxo"] {
				return nil, errors.New("ambiguous Fluxo primary group")
			}
			primary = true
		}
		for _, member := range strings.Split(v[3], ",") {
			if member == "fluxo" {
				groups = append(groups, v[0]+":"+strconv.Itoa(gid))
			}
		}
	}
	if !primary {
		return nil, errors.New("Fluxo primary group is not local")
	}
	sort.Strings(groups)
	result["fluxo_local_supplementary"] = strings.Join(groups, ",")
	// Supplementary membership is observed, not normalized: the existing installer
	// deliberately uses www-data and password-protected sudo. NSS systemd's
	// additional lookup code remains authenticated vendor lookup-only authority.
	return result, nil
}

func (p *pamCandidateInspection) optional(path string, limit int64) ([]byte, error) {
	raw, err := p.read(path, limit)
	if os.IsNotExist(err) {
		p.report.Files[path] = "absent"
		return nil, nil
	}
	return raw, err
}

func (p *pamCandidateInspection) revalidate() error {
	for path, before := range p.observed {
		now, err := pamCandidateObserve(path)
		if err != nil || before != now {
			if err == nil && pamSameProtectedDirectory(before, now) {
				return &pamDirectorySnapshotChanged{path, "during inspection"}
			}
			return fmt.Errorf("candidate input changed during inspection: %s", path)
		}
	}
	for path, hash := range p.report.Files {
		if hash == "absent" {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return fmt.Errorf("absent candidate input appeared: %s", path)
			}
		}
	}
	return nil
}

func (p *pamCandidateInspection) record(path string, raw []byte) {
	p.report.Files[path] = hashBytes(raw)
}
