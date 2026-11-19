package traffic

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxAPIPrefixes         = 16
	MaxAPIRoutes           = 32
	MaxAPIRuleIntervals    = 128
	MaxAPIRuleBytesPerSite = 64 << 10
	MaxAPIEndpointsPerHour = 64
	MaxAPIEndpointRows     = 50000
	MaxAPIEndpointResults  = 100
)

var ErrAPISettingsCapacity = errors.New("API settings capacity reached")

func DefaultAPISettings() APISettings {
	return APISettings{Prefixes: []string{"/api"}, Routes: []string{}}
}

// ValidateAPISettings accepts explicit, case-sensitive paths only. It never
// infers identifiers, decodes paths, or interprets regular expressions.
func ValidateAPISettings(settings APISettings) error {
	_, err := compileAPISettings(settings)
	return err
}

type apiRoute struct {
	path     string
	segments int
	literals int
}

type apiClassifier struct {
	prefixes []string
	routes   []apiRoute
}

func compileAPISettings(settings APISettings) (apiClassifier, error) {
	var out apiClassifier
	if len(settings.Prefixes) > MaxAPIPrefixes || len(settings.Routes) > MaxAPIRoutes {
		return out, fmt.Errorf("API settings allow at most %d prefixes and %d routes", MaxAPIPrefixes, MaxAPIRoutes)
	}
	seen := map[string]bool{}
	for _, prefix := range settings.Prefixes {
		if err := validAPIPath(prefix, false); err != nil {
			return out, fmt.Errorf("invalid API prefix %q: %w", prefix, err)
		}
		if seen[prefix] {
			return out, errors.New("duplicate API prefix")
		}
		seen[prefix] = true
		out.prefixes = append(out.prefixes, prefix)
	}
	for _, route := range settings.Routes {
		if err := validAPIPath(route, true); err != nil {
			return out, fmt.Errorf("invalid API route %q: %w", route, err)
		}
		r := apiRoute{path: route}
		for rest := strings.TrimPrefix(route, "/"); ; {
			segment, next, more := strings.Cut(rest, "/")
			r.segments++
			if !strings.HasPrefix(segment, ":") {
				r.literals++
			}
			if !more {
				break
			}
			rest = next
		}
		for _, prior := range out.routes {
			if prior.segments == r.segments && prior.literals == r.literals && apiRoutesOverlap(prior, r) {
				return out, fmt.Errorf("ambiguous API routes %q and %q", prior.path, r.path)
			}
		}
		out.routes = append(out.routes, r)
	}
	sort.Slice(out.routes, func(i, j int) bool {
		a, b := out.routes[i], out.routes[j]
		if a.literals != b.literals {
			return a.literals > b.literals
		}
		if a.segments != b.segments {
			return a.segments > b.segments
		}
		return a.path < b.path
	})
	return out, nil
}

func validAPIPath(value string, route bool) error {
	if len(value) == 0 || len(value) > MaxPathBytes || !utf8.ValidString(value) || value[0] != '/' || strings.Contains(value, "//") || len(value) > 1 && strings.HasSuffix(value, "/") || strings.ContainsAny(value, "?#\\*{}[]") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("use a clean absolute path of at most 512 bytes without trailing slash, query, whitespace or wildcards")
	}
	params := map[string]bool{}
	for _, segment := range strings.Split(value[1:], "/") {
		if segment == "." || segment == ".." {
			return errors.New("dot segments are not allowed")
		}
		if !strings.Contains(segment, ":") {
			continue
		}
		if !route || !strings.HasPrefix(segment, ":") || !validAPIParameter(segment[1:]) || params[segment[1:]] {
			return errors.New("parameters must be unique whole :name segments with ASCII letters, digits or underscore")
		}
		params[segment[1:]] = true
	}
	return nil
}

func validAPIParameter(v string) bool {
	if v == "" {
		return false
	}
	for i, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func apiRoutesOverlap(a, b apiRoute) bool {
	aRest, bRest := a.path[1:], b.path[1:]
	for {
		aSegment, aNext, aMore := strings.Cut(aRest, "/")
		bSegment, bNext, bMore := strings.Cut(bRest, "/")
		if !strings.HasPrefix(aSegment, ":") && !strings.HasPrefix(bSegment, ":") && aSegment != bSegment {
			return false
		}
		if aMore != bMore {
			return false
		}
		if !aMore {
			return true
		}
		aRest, bRest = aNext, bNext
	}
}

func (c apiClassifier) classify(path string) (string, bool) {
	for _, route := range c.routes {
		if apiRouteMatches(route.path, path) {
			return route.path, true
		}
	}
	for _, prefix := range c.prefixes {
		if prefix == "/" || path == prefix || len(path) > len(prefix) && strings.HasPrefix(path, prefix) && path[len(prefix)] == '/' {
			return path, true
		}
	}
	return "", false
}

// Scan route segments without retaining a []string per historical route or
// allocating per-request segment arrays. The retained rule text has a separate
// per-site byte budget, in addition to the interval-count bound.
func apiRouteMatches(route, path string) bool {
	routeRest, pathRest := strings.TrimPrefix(route, "/"), strings.TrimPrefix(path, "/")
	for {
		segment, routeNext, routeMore := strings.Cut(routeRest, "/")
		value, pathNext, pathMore := strings.Cut(pathRest, "/")
		if strings.HasPrefix(segment, ":") {
			if value == "" {
				return false
			}
		} else if segment != value {
			return false
		}
		if routeMore != pathMore {
			return false
		}
		if !routeMore {
			return true
		}
		routeRest, pathRest = routeNext, pathNext
	}
}

func apiMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return method
	default:
		return "OTHER"
	}
}
