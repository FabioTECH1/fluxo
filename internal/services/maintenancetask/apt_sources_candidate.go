package maintenancetask

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// This candidate admits effective source URI protocols, not repository trust or
// package-install permission. It records only hashes/membership/counts, never
// credentials or URI strings. It is unselected by production until the method
// startup/redirect/guard proof is accepted for the exact APT build.
func inspectAPTSourceInputs(p *profile) error {
	const main = "/etc/apt/sources.list"
	const parts = "/etc/apt/sources.list.d"
	count := 0
	raw, err := readRegular(main, 8<<20)
	if os.IsNotExist(err) {
		p.Files[main] = "absent"
	} else if err != nil {
		return err
	} else {
		n, err := validateAPTClassicSources(raw)
		if err != nil {
			return fmt.Errorf("APT source format/protocol needs review: %s", main)
		}
		count += n
		p.Files[main] = digest(raw)
	}
	entries, err := p.directory(parts, false)
	if err != nil {
		return err
	}
	if len(entries) > 128 {
		return errors.New("APT source directory needs compatibility review")
	}
	for _, entry := range entries {
		path := filepath.Join(parts, entry.Name())
		raw, err := readRegular(path, 8<<20)
		if err != nil {
			return err
		}
		// Retain protected-file and drift evidence even for inert backups. APT's
		// ReadSourceDir passes only the final extensions list/sources to its
		// parser; Ignore-Files-Silently changes notices, not that selection.
		p.Files[path] = digest(raw)
		n, err := validateAPTSourcePart(entry.Name(), raw)
		if err != nil {
			return fmt.Errorf("APT source format/protocol needs review: %s", path)
		}
		count += n
	}
	p.External["apt-source-protocol-contract"] = "http-https-file-candidate-v1"
	p.External["apt-source-uri-count"] = fmt.Sprint(count)
	return nil
}

func validateAPTSourcePart(name string, raw []byte) (int, error) {
	if !utf8.ValidString(name) {
		return 0, errors.New("APT source filename is not representable in retained evidence")
	}
	switch filepath.Ext(name) {
	case ".list":
		return validateAPTClassicSources(raw)
	case ".sources":
		return validateAPTDeb822Sources(raw)
	default:
		// This includes .orig/.save/.disabled and Ubuntu installer's
		// .sources.curtin.orig. Their bytes are never parsed as source entries.
		return 0, nil
	}
}

func validateAPTSourceURI(raw string) error {
	bad := errors.New("unreviewed APT source URI")
	if strings.ContainsAny(raw, "\x00\r\n\t ") {
		return bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(u.Path, "\x00\r\n\t") {
		return bad
	}
	switch u.Scheme {
	case "http", "https":
		if u.Hostname() == "" || strings.ContainsAny(u.Host, "\x00\r\n\t ") {
			return bad
		}
	case "file":
		if u.User != nil || u.RawQuery != "" || u.Host != "" && u.Host != "localhost" || !filepath.IsAbs(u.Path) {
			return bad
		}
	default:
		return bad
	}
	return nil
}

func validateAPTClassicSources(raw []byte) (int, error) {
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Refuse ambiguous inline-comment/quoted variants rather than guessing
		// where APT's parser would terminate a credential-bearing URI.
		if strings.ContainsAny(line, "#\"'\\\x00") {
			return 0, errors.New("unreviewed source-list syntax")
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "deb" && fields[0] != "deb-src" {
			return 0, errors.New("unreviewed source-list type")
		}
		uri := 1
		if strings.HasPrefix(fields[1], "[") {
			end := 1
			for end < len(fields) && !strings.HasSuffix(fields[end], "]") {
				end++
			}
			if end >= len(fields) {
				return 0, errors.New("unclosed source-list options")
			}
			options := strings.TrimSuffix(strings.TrimPrefix(strings.Join(fields[1:end+1], " "), "["), "]")
			if strings.ContainsAny(options, "[]") {
				return 0, errors.New("unreviewed source-list options")
			}
			for _, option := range strings.Fields(options) {
				key, value, ok := strings.Cut(option, "=")
				if !ok || value == "" || !aptSourceOption(key) {
					return 0, errors.New("unreviewed source-list option")
				}
			}
			uri = end + 1
		}
		if uri+1 >= len(fields) {
			return 0, errors.New("incomplete source-list entry")
		}
		if err := validateAPTSourceURI(fields[uri]); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func aptSourceOption(key string) bool {
	switch strings.ToLower(key) {
	case "arch", "arch+", "arch-", "signed-by", "trusted", "allow-insecure", "allow-weak", "allow-downgrade-to-insecure", "check-valid-until", "valid-until-min", "valid-until-max", "check-date", "date-max-future", "by-hash", "pdiffs", "lang", "target":
		return true
	}
	return false
}

func validateAPTDeb822Sources(raw []byte) (int, error) {
	count := 0
	fields := map[string]string{}
	last := ""
	finish := func() error {
		if len(fields) == 0 {
			return nil
		}
		types := strings.Fields(fields["types"])
		if len(types) == 0 {
			return errors.New("missing source stanza types")
		}
		for _, kind := range types {
			if kind != "deb" && kind != "deb-src" {
				return errors.New("unreviewed source stanza type")
			}
		}
		enabled := strings.ToLower(fields["enabled"])
		if enabled != "" && enabled != "yes" && enabled != "no" && enabled != "true" && enabled != "false" && enabled != "1" && enabled != "0" {
			return errors.New("unreviewed source enabled state")
		}
		// APT's Type::ParseStanza checks Enabled before processing URIs.
		if enabled == "no" || enabled == "false" || enabled == "0" {
			return nil
		}
		uris := strings.Fields(fields["uris"])
		if len(uris) == 0 || strings.TrimSpace(fields["suites"]) == "" {
			return errors.New("incomplete source stanza")
		}
		for _, uri := range uris {
			if err := validateAPTSourceURI(uri); err != nil {
				return err
			}
			count++
		}
		return nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.ContainsRune(line, '\x00') {
			return 0, errors.New("invalid source stanza")
		}
		if strings.TrimSpace(line) == "" {
			if err := finish(); err != nil {
				return 0, err
			}
			fields = map[string]string{}
			last = ""
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if last == "" {
				return 0, errors.New("unreviewed source continuation")
			}
			fields[last] += " " + strings.TrimSpace(line)
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		key = strings.ToLower(key)
		if !ok || !aptDeb822Field(key) {
			return 0, errors.New("unreviewed source stanza field")
		}
		if _, duplicate := fields[key]; duplicate {
			return 0, errors.New("duplicate source stanza field")
		}
		fields[key] = strings.TrimSpace(value)
		last = key
	}
	if err := finish(); err != nil {
		return 0, err
	}
	return count, nil
}

func aptDeb822Field(key string) bool {
	switch key {
	case "types", "uris", "suites", "components", "architectures", "architectures-add", "architectures-remove", "languages", "targets", "pdiffs", "by-hash", "allow-insecure", "allow-weak", "allow-downgrade-to-insecure", "trusted", "check-valid-until", "valid-until-min", "valid-until-max", "check-date", "date-max-future", "signed-by", "enabled", "x-repolib-name":
		return true
	}
	return false
}
