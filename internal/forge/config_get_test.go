package forge_test

import (
	"errors"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

func TestGet(t *testing.T) {
	args := []string{"config", "get", "user.email"}
	tests := []struct {
		name    string
		call    jjtest.Call
		want    string
		wantErr bool
	}{
		{name: "set", call: jjtest.Call{Args: args, Output: jjtest.Output("me@example.com\n")}, want: "me@example.com"},
		{name: "unset", call: jjtest.Call{Args: args, Err: jjtest.ConfigNotFound("user.email")}},
		{name: "other error", call: jjtest.Call{Args: args, Err: errors.New("Config error: invalid TOML")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(), tt.call)
			got, err := forge.NewConfigManager(scenario.Client()).Get("user.email")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Get() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Get() = %q, want %q", got, tt.want)
			}
			scenario.Verify()
		})
	}
}
