package config

import (
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/compose"
)

func TestValidateRolloutServices(t *testing.T) {
	healthy := compose.Service{Image: "nginx:1.25", Healthcheck: map[string]any{"test": "true"}}
	fileWith := func(svc compose.Service) *compose.File {
		return &compose.File{Services: map[string]compose.Service{"web": svc}}
	}

	tests := []struct {
		name       string
		file       *compose.File
		services   []string
		wantErr    bool
		wantSubstr string
	}{
		{"unknown service", fileWith(healthy), []string{"api"}, true, "is not defined"},
		{"published ports", fileWith(compose.Service{Image: "nginx:1.25", Ports: []any{"8080:80"}, Healthcheck: map[string]any{"test": "true"}}), []string{"web"}, true, "publishes host ports"},
		{"container_name", fileWith(compose.Service{Image: "nginx:1.25", ContainerName: "web", Healthcheck: map[string]any{"test": "true"}}), []string{"web"}, true, "container_name"},
		{"no healthcheck", fileWith(compose.Service{Image: "nginx:1.25"}), []string{"web"}, true, "no healthcheck"},
		{"valid", fileWith(healthy), []string{"web"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRolloutServices(tt.services, tt.file)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s", tt.name)
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("error %q should mention %q", err.Error(), tt.wantSubstr)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
