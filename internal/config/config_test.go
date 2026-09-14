package config

import (
	"errors"
	"flag"
	"io"
	"slices"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, showVersion, err := Parse("test", nil, io.Discard)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if showVersion {
		t.Error("Parse() asked for the version without -version")
	}
	if !cfg.Force {
		t.Error("Force = false, want true")
	}
	if cfg.SecretName != "image-pull-secret" {
		t.Errorf("SecretName = %q, want %q", cfg.SecretName, "image-pull-secret")
	}
	if want := []string{DefaultServiceAccountName}; !slices.Equal(cfg.ServiceAccounts, want) {
		t.Errorf("ServiceAccounts = %v, want %v", cfg.ServiceAccounts, want)
	}
	if cfg.LoopDuration != 10*time.Second {
		t.Errorf("LoopDuration = %s, want 10s", cfg.LoopDuration)
	}
	if len(cfg.ExcludedNamespaces) != 0 {
		t.Errorf("ExcludedNamespaces = %v, want empty", cfg.ExcludedNamespaces)
	}
}

func TestParseFlagsBeatEnv(t *testing.T) {
	t.Setenv("CONFIG_SECRETNAME", "from-env")
	t.Setenv("CONFIG_DEBUG", "true")
	t.Setenv("CONFIG_LOOP_DURATION", "1m")

	cfg, _, err := Parse("test", []string{"-secretname", "from-flag"}, io.Discard)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.SecretName != "from-flag" {
		t.Errorf("SecretName = %q, want the flag value %q", cfg.SecretName, "from-flag")
	}
	if !cfg.Debug {
		t.Error("Debug = false, want the env value true")
	}
	if cfg.LoopDuration != time.Minute {
		t.Errorf("LoopDuration = %s, want the env value 1m", cfg.LoopDuration)
	}
}

func TestParseLists(t *testing.T) {
	cfg, _, err := Parse("test", []string{
		"-excluded-namespaces", "kube-system, kube-public ,",
		"-serviceaccounts", "default,builder",
	}, io.Discard)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if want := []string{"kube-system", "kube-public"}; !slices.Equal(cfg.ExcludedNamespaces, want) {
		t.Errorf("ExcludedNamespaces = %v, want %v", cfg.ExcludedNamespaces, want)
	}
	if want := []string{"default", "builder"}; !slices.Equal(cfg.ServiceAccounts, want) {
		t.Errorf("ServiceAccounts = %v, want %v", cfg.ServiceAccounts, want)
	}
}

func TestParseVersion(t *testing.T) {
	_, showVersion, err := Parse("test", []string{"-version"}, io.Discard)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !showVersion {
		t.Error("Parse(-version) did not ask for the version")
	}
}

func TestParseHelp(t *testing.T) {
	if _, _, err := Parse("test", []string{"-help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("Parse(-help) error = %v, want flag.ErrHelp", err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "both credential sources",
			args: []string{"-dockerconfigjson", "{}", "-dockerconfigjsonpath", "/tmp/creds"},
		},
		{
			name: "empty secret name",
			args: []string{"-secretname", ""},
		},
		{
			name: "non-positive loop duration",
			args: []string{"-loop-duration", "0s"},
		},
		{
			name: "no service accounts",
			args: []string{"-serviceaccounts", ""},
		},
		{
			name: "unknown log format",
			args: []string{"-log-format", "logfmt"},
		},
		{
			name: "unknown flag",
			args: []string{"-nope"},
		},
		{
			name: "positional argument",
			args: []string{"unexpected"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Parse("test", tc.args, io.Discard); err == nil {
				t.Errorf("Parse(%v) succeeded, want an error", tc.args)
			}
		})
	}
}

func TestParseAllowsEmptyServiceAccountsWithAllServiceAccount(t *testing.T) {
	cfg, _, err := Parse("test", []string{"-serviceaccounts", "", "-allserviceaccount"}, io.Discard)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.ServiceAccountSelected("anything") {
		t.Error("ServiceAccountSelected() = false with -allserviceaccount, want true")
	}
}

func TestParseAllowsZeroLoopDurationWithRunOnce(t *testing.T) {
	if _, _, err := Parse("test", []string{"-loop-duration", "0s", "-runonce"}, io.Discard); err != nil {
		t.Errorf("Parse() error = %v, want nil for -runonce", err)
	}
}

func TestNamespaceExcluded(t *testing.T) {
	cfg := &Config{ExcludedNamespaces: []string{"kube-system", "other-namespace"}}
	if !cfg.NamespaceExcluded("kube-system") {
		t.Error("NamespaceExcluded(kube-system) = false, want true")
	}
	if cfg.NamespaceExcluded("default") {
		t.Error("NamespaceExcluded(default) = true, want false")
	}
	if (&Config{}).NamespaceExcluded("default") {
		t.Error("NamespaceExcluded(default) = true with no exclusions, want false")
	}
}

func TestServiceAccountSelected(t *testing.T) {
	cfg := &Config{ServiceAccounts: []string{"default", "builder"}}
	if !cfg.ServiceAccountSelected("builder") {
		t.Error("ServiceAccountSelected(builder) = false, want true")
	}
	if cfg.ServiceAccountSelected("other") {
		t.Error("ServiceAccountSelected(other) = true, want false")
	}
}
