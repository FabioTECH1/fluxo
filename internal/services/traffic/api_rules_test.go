package traffic

import (
	"fmt"
	"strings"
	"testing"
)

func TestAPIClassificationUsesBoundariesAndExplicitRoutes(t *testing.T) {
	c, err := compileAPISettings(APISettings{Prefixes: []string{"/api", "/custom"}, Routes: []string{"/outside/:id", "/api/users/:id", "/api/users/me"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, endpoint string
		match          bool
	}{
		{"/api", "/api", true}, {"/api/", "/api/", true}, {"/api/users/42", "/api/users/:id", true}, {"/api/users/abc", "/api/users/:id", true},
		{"/api/users/me", "/api/users/me", true}, {"/api/users/42/posts", "/api/users/42/posts", true}, {"/outside/opaque-ID", "/outside/:id", true},
		{"/apiculture", "", false}, {"/apiary/users", "", false}, {"/API/users", "", false}, {"/graphql", "", false}, {"/outside/", "", false},
		{"/custom/path", "/custom/path", true}, {"/customized", "", false},
	} {
		got, match := c.classify(test.path)
		if got != test.endpoint || match != test.match {
			t.Errorf("%q => %q,%v; want %q,%v", test.path, got, match, test.endpoint, test.match)
		}
	}
	// More literal segments win independently of the order provided.
	for _, routes := range [][]string{{"/:a/:b", "/outside/:id"}, {"/outside/:id", "/:a/:b"}} {
		c, err = compileAPISettings(APISettings{Routes: routes})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := c.classify("/outside/42"); got != "/outside/:id" {
			t.Fatalf("non-deterministic precedence: %q", got)
		}
	}
}

func TestAPISettingsRejectAmbiguousOrUnboundedRules(t *testing.T) {
	bad := []APISettings{
		{Prefixes: []string{"api"}}, {Prefixes: []string{"/api/"}}, {Prefixes: []string{"/api", "/api"}}, {Prefixes: []string{"/:id"}},
		{Prefixes: []string{"/a//b"}}, {Prefixes: []string{"/a/../b"}}, {Prefixes: []string{"/a?secret=x"}}, {Prefixes: []string{"/a#x"}},
		{Prefixes: []string{"/" + strings.Repeat("x", MaxPathBytes)}}, {Routes: []string{"/a/*"}}, {Routes: []string{"/a/{id}"}},
		{Routes: []string{"/a/:"}}, {Routes: []string{"/a/:1id"}}, {Routes: []string{"/a/:id/:id"}}, {Routes: []string{"/a/x:id"}},
		{Routes: []string{"/a/:id", "/a/:slug"}}, {Routes: []string{"/x/:id/view", "/x/admin/:verb"}},
		{Routes: []string{"/x/:a/:b", "/x/:c/:d"}}, {Routes: []string{"/literal", "/literal"}},
		{Prefixes: []string{"/a b"}}, {Prefixes: []string{"/a\x00b"}}, {Routes: []string{"/a\\b"}},
	}
	for _, settings := range bad {
		if err := ValidateAPISettings(settings); err == nil {
			t.Errorf("accepted invalid settings: %+v", settings)
		}
	}
	for _, kind := range []string{"prefixes", "routes"} {
		settings := APISettings{}
		limit := MaxAPIPrefixes
		if kind == "routes" {
			limit = MaxAPIRoutes
		}
		for i := 0; i < limit; i++ {
			if kind == "routes" {
				settings.Routes = append(settings.Routes, fmt.Sprintf("/r%d/:id", i))
			} else {
				settings.Prefixes = append(settings.Prefixes, fmt.Sprintf("/p%d", i))
			}
		}
		if err := ValidateAPISettings(settings); err != nil {
			t.Fatal(err)
		}
		if kind == "routes" {
			settings.Routes = append(settings.Routes, "/too-many")
		} else {
			settings.Prefixes = append(settings.Prefixes, "/too-many")
		}
		if err := ValidateAPISettings(settings); err == nil {
			t.Errorf("accepted too many %s", kind)
		}
	}
	for _, settings := range []APISettings{{}, {Prefixes: []string{"/"}}, {Routes: []string{"/", "/a/:_id2"}}} {
		if err := ValidateAPISettings(settings); err != nil {
			t.Fatalf("rejected explicit valid settings %+v: %v", settings, err)
		}
	}
}

func TestAPIClassifierDoesNotAllocatePerSegmentOrRequest(t *testing.T) {
	settings := APISettings{Prefixes: []string{"/" + strings.Repeat("p", 511)}}
	for i := 0; i < MaxAPIRoutes; i++ {
		settings.Routes = append(settings.Routes, strings.Repeat("/a", 254)+fmt.Sprintf("/r%02d", i))
	}
	classifier, err := compileAPISettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{settings.Routes[MaxAPIRoutes-1], settings.Prefixes[0] + "/child", strings.Repeat("/a", 254) + "/miss"} {
		if allocations := testing.AllocsPerRun(20, func() { classifier.classify(path) }); allocations != 0 {
			t.Fatalf("maximum-length route matching allocated %.0f segment/path objects", allocations)
		}
	}
}
