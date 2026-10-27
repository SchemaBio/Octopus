package router

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"testing"
)

func TestExampleRoutesAreRegisteredOnlyForSaaS(t *testing.T) {
	for _, edition := range []string{"opensource", "saas"} {
		cfg := &config.Config{Edition: edition}
		cfg.Server.Mode = "test"
		r := New(cfg)
		found := map[string]bool{}
		for _, route := range r.Routes() {
			found[route.Path] = true
		}
		for _, path := range []string{"/api/v1/internal/example-data/template", "/api/v1/internal/example-data/grants", "/api/v1/example-data"} {
			if found[path] != (edition == "saas") {
				t.Fatalf("%s route %s present=%v", edition, path, found[path])
			}
		}
	}
}
