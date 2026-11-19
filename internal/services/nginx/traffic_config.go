package nginx

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const trafficMarker = "# Fluxo traffic analytics v1"
const trafficSavedOffMarker = "# Fluxo traffic analytics v1 saved-off "
const trafficEndMarker = "# End Fluxo traffic analytics v1"
const trafficHTTPHeader = "# Managed by Fluxo. Traffic analytics v1.\n"

var trafficGenerationPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,96}$`)
var trafficConfigNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}$`)
var trafficPathPattern = regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`)

func trafficFormat(site TrafficSite) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(site.ID, 10) + ":" + site.Generation))
	return "fluxo_traffic_" + hex.EncodeToString(sum[:12])
}

// Every variable in the JSON record is either numeric or JSON escaped. The
// original URI is stripped before logging, including on internal redirects.
// Raw address, user agent, query, and referer variables never enter the record.
const trafficMaps = `map $request_method $fluxo_traffic_method {
    default OTHER;
    GET GET;
    HEAD HEAD;
    POST POST;
    PUT PUT;
    PATCH PATCH;
    DELETE DELETE;
    OPTIONS OPTIONS;
}
map $request_uri $fluxo_traffic_path {
    default "";
    "~^(?<fluxo_traffic_original_path>/[^?]{0,1023})(?:\\?|$)" $fluxo_traffic_original_path;
}
map $http_user_agent $fluxo_traffic_bot {
    default 0;
    ~*(bot|crawler|spider|slurp|bingpreview|headless|curl|wget|python-requests) 1;
}
map $http_accept $fluxo_traffic_html {
    default 0;
    ~*(text/html|application/xhtml\\+xml) 1;
}
map $http_referer $fluxo_traffic_referrer_candidate {
    default "";
    "~*^https?://[0-9.]+(?::[0-9]+)?(?:[/?#]|$)" "";
    "~*^https?://(?<fluxo_traffic_referrer_host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*)(?::[0-9]{1,5})?(?:[/?#]|$)" $fluxo_traffic_referrer_host;
}
map $fluxo_traffic_referrer_candidate $fluxo_traffic_referrer {
    default "";
    "~^.{1,253}$" $fluxo_traffic_referrer_candidate;
}
`

func renderTrafficHTTP(sites []TrafficSite, socket, operation string) string {
	var out strings.Builder
	out.WriteString(trafficHTTPHeader)
	if len(sites) > 0 {
		out.WriteString(trafficMaps)
		for _, site := range sites {
			fmt.Fprintf(&out, "log_format %s escape=json '{\"v\":1,\"generation\":\"%s\",\"ts\":$msec,\"method\":\"$fluxo_traffic_method\",\"path\":\"$fluxo_traffic_path\",\"status\":$status,\"bytes\":$body_bytes_sent,\"request_time\":$request_time,\"bot\":$fluxo_traffic_bot,\"html\":$fluxo_traffic_html,\"referrer\":\"$fluxo_traffic_referrer\"}';\n", trafficFormat(site), site.Generation)
		}
	}
	// This socket is reachable only by the root control plane. A fresh marker
	// proves the complete transaction was loaded, including OFF transactions.
	fmt.Fprintf(&out, "server {\n    listen unix:%s;\n    server_name fluxo-traffic-control;\n    access_log off;\n    location = /activation { return 200 '%s'; }\n    location / { return 404; }\n}\n", socket, operation)
	return out.String()
}

type trafficToken struct {
	value      string
	start, end int
}
type trafficBlock struct {
	name               string
	start, open, close int
	directives         [][]trafficToken
	directiveEnds      []int
	children           []*trafficBlock
}

// This deliberately small lexer preserves all original bytes. Unsupported
// nested constructs are rejected rather than round-tripped through a renderer.
func lexTrafficConfig(raw string) ([]trafficToken, error) {
	var tokens []trafficToken
	for i := 0; i < len(raw); {
		if strings.ContainsRune(" \t\r\n", rune(raw[i])) {
			i++
			continue
		}
		if raw[i] == '#' {
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if strings.ContainsRune("{};", rune(raw[i])) {
			tokens = append(tokens, trafficToken{string(raw[i]), i, i + 1})
			i++
			continue
		}
		var value strings.Builder
		quote := byte(0)
		for i < len(raw) {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				value.WriteByte(c)
				value.WriteByte(raw[i+1])
				i += 2
				continue
			}
			if quote != 0 {
				if c == quote {
					quote = 0
					i++
					continue
				}
				value.WriteByte(c)
				i++
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				i++
				continue
			}
			if strings.ContainsRune(" \t\r\n{};#", rune(c)) {
				break
			}
			value.WriteByte(c)
			i++
		}
		if quote != 0 || i == start {
			return nil, errors.New("unsupported or unterminated Nginx syntax")
		}
		tokens = append(tokens, trafficToken{value.String(), start, i})
	}
	return tokens, nil
}

func parseTrafficBlocks(tokens []trafficToken) (*trafficBlock, error) {
	return parseTrafficSyntax(tokens, true)
}

func parseTrafficSyntax(tokens []trafficToken, vhost bool) (*trafficBlock, error) {
	root := &trafficBlock{name: "root", close: -1}
	stack := []*trafficBlock{root}
	var pending []trafficToken
	for _, token := range tokens {
		current := stack[len(stack)-1]
		switch token.value {
		case "{":
			if len(pending) == 0 {
				return nil, errors.New("Nginx block has no directive")
			}
			name := pending[0].value
			if vhost && name != "server" && name != "location" && name != "if" && name != "limit_except" {
				return nil, fmt.Errorf("unsupported Nginx block %q", name)
			}
			if vhost && (name == "server" && current != root || name != "server" && current == root) {
				return nil, errors.New("unsupported Nginx block nesting")
			}
			child := &trafficBlock{name: name, start: pending[0].start, open: token.end, close: -1}
			current.children = append(current.children, child)
			stack = append(stack, child)
			pending = nil
		case ";":
			if len(pending) == 0 {
				return nil, errors.New("empty Nginx directive")
			}
			current.directives = append(current.directives, pending)
			current.directiveEnds = append(current.directiveEnds, token.end)
			pending = nil
		case "}":
			if len(stack) == 1 || len(pending) > 0 {
				return nil, errors.New("unbalanced Nginx block")
			}
			current.close = token.start
			stack = stack[:len(stack)-1]
		default:
			pending = append(pending, token)
		}
	}
	if len(stack) != 1 || len(pending) > 0 || vhost && (len(root.directives) > 0 || len(root.children) == 0) {
		return nil, errors.New("traffic analytics requires complete server blocks only")
	}
	return root, nil
}

func stripTrafficConfig(raw string) (string, error) {
	for {
		start := strings.Index(raw, "\n"+trafficMarker+"\n")
		if start < 0 {
			break
		}
		end := strings.Index(raw[start+1:], "\n"+trafficEndMarker+"\n")
		if end < 0 {
			return "", errors.New("incomplete traffic analytics marker")
		}
		end = start + 1 + end + len("\n"+trafficEndMarker+"\n")
		lines := strings.Split(raw[start:end], "\n")
		if len(lines) < 5 || !strings.HasPrefix(lines[len(lines)-3], "    access_log ") || !strings.Contains(lines[len(lines)-3], " fluxo_traffic_") {
			return "", errors.New("modified traffic analytics block; refusing to remove custom configuration")
		}
		for _, line := range lines[2 : len(lines)-2] {
			if !strings.HasPrefix(line, "    access_log ") || !strings.HasSuffix(line, ";") {
				return "", errors.New("modified traffic analytics block; refusing to remove custom configuration")
			}
			tokens, err := lexTrafficConfig(line)
			if err != nil || len(tokens) < 3 || tokens[0].value != "access_log" || tokens[len(tokens)-1].value != ";" {
				return "", errors.New("invalid managed traffic access_log directive")
			}
			for _, token := range tokens[1 : len(tokens)-1] {
				if token.value == ";" || token.value == "{" || token.value == "}" {
					return "", errors.New("extra directive inside traffic analytics marker")
				}
			}
		}
		raw = raw[:start] + raw[end:]
	}
	for {
		start := strings.Index(raw, "\n"+trafficSavedOffMarker)
		if start < 0 {
			break
		}
		payloadStart := start + len("\n"+trafficSavedOffMarker)
		length := strings.IndexByte(raw[payloadStart:], '\n')
		if length < 0 || length > 128<<10 {
			return "", errors.New("invalid saved traffic access_log off marker")
		}
		end := payloadStart + length + 1
		original, err := base64.RawStdEncoding.DecodeString(raw[payloadStart : payloadStart+length])
		if err != nil || len(original) > 64<<10 {
			return "", errors.New("invalid saved traffic access_log off encoding")
		}
		tokens, err := lexTrafficConfig(string(original))
		if err != nil || len(tokens) != 3 || tokens[0].value != "access_log" || tokens[1].value != "off" || tokens[2].value != ";" {
			return "", errors.New("saved analytics directive is not exactly access_log off")
		}
		raw = raw[:start] + string(original) + raw[end:]
	}
	if strings.Contains(raw, trafficMarker) || strings.Contains(raw, trafficEndMarker) {
		return "", errors.New("invalid traffic analytics marker; restore the managed block before retrying")
	}
	return raw, nil
}

func injectTrafficConfig(raw string, site TrafficSite, inherited []string) (string, error) {
	clean, err := stripTrafficConfig(raw)
	if err != nil {
		return "", err
	}
	tokens, err := lexTrafficConfig(clean)
	if err != nil {
		return "", err
	}
	root, err := parseTrafficBlocks(tokens)
	if err != nil {
		return "", err
	}
	type insertion struct {
		position, end int
		operational   []string
		replacement   string
	}
	var positions []insertion
	var walk func(*trafficBlock) error
	walk = func(block *trafficBlock) error {
		hasAccess, off := false, false
		logCount, offIndex := 0, -1
		for index, directive := range block.directives {
			switch directive[0].value {
			case "access_log":
				hasAccess = true
				logCount++
				if len(directive) > 1 && directive[1].value == "off" {
					off = true
					offIndex = index
				}
			case "include":
				// The generated PHP templates use the distribution's parameter-only
				// fastcgi_params file. Other includes can hide logging overrides.
				if len(directive) != 2 || directive[1].value != "fastcgi_params" {
					return fmt.Errorf("unsupported custom Nginx include: analytics cannot prove logging coverage")
				}
			}
		}
		if block.name == "server" {
			hasName := false
			for _, directive := range block.directives {
				if directive[0].value == "server_name" {
					hasName = true
				}
			}
			if !hasName {
				return errors.New("traffic analytics requires explicit server_name")
			}

			if !hasAccess && len(inherited) == 0 {
				return errors.New("cannot determine inherited operational access_log policy")
			}
			var operational []string
			if !hasAccess {
				inheritedOff := false
				for _, line := range inherited {
					if line == "access_log off;" {
						inheritedOff = true
					}
				}
				if !inheritedOff {
					operational = append(operational, inherited...)
				}
			}
			positions = append(positions, insertion{position: block.open, end: block.open, operational: operational})
		} else if hasAccess {
			positions = append(positions, insertion{position: block.open, end: block.open})
		}
		if off {
			if logCount != 1 || offIndex < 0 || len(block.directives[offIndex]) != 2 {
				return errors.New("multiple access_log directives combined with off require operator review")
			}
			directive := block.directives[offIndex]
			original := clean[directive[0].start:block.directiveEnds[offIndex]]
			if len(original) > 64<<10 {
				return errors.New("access_log off directive exceeded its preservation limit")
			}
			positions = append(positions, insertion{position: directive[0].start, end: block.directiveEnds[offIndex], replacement: "\n" + trafficSavedOffMarker + base64.RawStdEncoding.EncodeToString([]byte(original)) + "\n"})
		}
		for _, child := range block.children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, server := range root.children {
		if err := walk(server); err != nil {
			return "", err
		}
	}
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].position == positions[j].position {
			return positions[i].end > positions[j].end
		}
		return positions[i].position > positions[j].position
	})
	for _, insert := range positions {
		if insert.replacement != "" {
			clean = clean[:insert.position] + insert.replacement + clean[insert.end:]
			continue
		}
		snippet := "\n" + trafficMarker + "\n"
		for _, line := range insert.operational {
			snippet += "    " + line + "\n"
		}
		snippet += "    access_log " + site.LogPath + " " + trafficFormat(site) + ";\n" + trafficEndMarker + "\n"
		clean = clean[:insert.position] + snippet + clean[insert.position:]
	}
	return clean, nil
}

// StripTrafficConfig removes only the additive managed logging blocks, keeping
// the original vhost byte-for-byte. Inspection/qualification may use this when
// comparing a managed runtime template with its analytics-free contract.
func StripTrafficConfig(raw string) (string, error) { return stripTrafficConfig(raw) }

type trafficDumpFile struct {
	path, raw string
	tree      *trafficBlock
}

// nginx -T expands includes into individually annotated files. Reconstruct only
// the HTTP logging inheritance and validate parameter-only fastcgi includes;
// never infer loaded worker state from this on-disk configuration dump.
func trafficInheritedLogs(dump string) ([]string, error) {
	const marker = "# configuration file "
	var files []trafficDumpFile
	for _, part := range strings.Split(dump, marker)[1:] {
		line, raw, ok := strings.Cut(part, "\n")
		if !ok || !strings.HasSuffix(line, ":") {
			return nil, errors.New("unsupported nginx -T output")
		}
		tokens, err := lexTrafficConfig(raw)
		if err != nil {
			return nil, err
		}
		tree, err := parseTrafficSyntax(tokens, false)
		if err != nil {
			return nil, err
		}
		files = append(files, trafficDumpFile{strings.TrimSuffix(line, ":"), raw, tree})
	}
	if len(files) == 0 {
		return nil, errors.New("nginx -T did not expose its HTTP logging policy")
	}
	var http *trafficBlock
	for _, child := range files[0].tree.children {
		if child.name == "http" {
			if http != nil {
				return nil, errors.New("multiple HTTP configurations are unsupported")
			}
			http = child
		}
	}
	if http == nil {
		return nil, errors.New("nginx HTTP configuration was not found")
	}
	var logs []string
	var expand func(trafficDumpFile, *trafficBlock, int) error
	expand = func(file trafficDumpFile, block *trafficBlock, depth int) error {
		if depth > 12 {
			return errors.New("Nginx logging include nesting is too deep")
		}
		for _, d := range block.directives {
			if d[0].value == "access_log" {
				var parts []string
				for _, token := range d {
					rawToken := file.raw[token.start:token.end]
					if strings.ContainsAny(rawToken, "\r\n") {
						return errors.New("multiline quoted operational log arguments are unsupported")
					}
					parts = append(parts, rawToken)
				}
				logs = append(logs, strings.Join(parts, " ")+";")
			}
			if d[0].value == "include" {
				if len(d) != 2 {
					return errors.New("unsupported Nginx include syntax")
				}
				pattern := d[1].value
				if !strings.HasPrefix(pattern, "/") {
					pattern = files[0].path[:strings.LastIndex(files[0].path, "/")+1] + pattern
				}
				for _, included := range files {
					match, err := filepath.Match(pattern, included.path)
					if err != nil {
						return err
					}
					if !match {
						continue
					}
					// A relative parameter include must never hide access_log overrides.
					if filepath.Base(included.path) == "fastcgi_params" {
						if len(included.tree.children) > 0 {
							return errors.New("custom fastcgi_params blocks are unsupported for traffic analytics")
						}
						for _, param := range included.tree.directives {
							if param[0].value != "fastcgi_param" {
								return errors.New("custom fastcgi_params directives are unsupported for traffic analytics")
							}
						}
					}
					if err := expand(included, included.tree, depth+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := expand(files[0], http, 0); err != nil {
		return nil, err
	}
	// FastCGI parameter files included in nested locations are not reached by the
	// HTTP inheritance expansion, so check those independently as well.
	for _, file := range files {
		if filepath.Base(file.path) == "fastcgi_params" {
			if len(file.tree.children) > 0 {
				return nil, errors.New("custom fastcgi_params blocks are unsupported")
			}
			for _, d := range file.tree.directives {
				if d[0].value != "fastcgi_param" {
					return nil, errors.New("custom fastcgi_params directives are unsupported")
				}
			}
		}
	}
	if len(logs) == 0 {
		return nil, errors.New("traffic analytics requires an explicit HTTP access_log policy; implicit compiled-prefix defaults are unsupported")
	}
	return logs, nil
}
