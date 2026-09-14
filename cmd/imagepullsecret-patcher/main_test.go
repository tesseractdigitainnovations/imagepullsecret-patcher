package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/config"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer

	if err := run([]string{"imagepullsecret-patcher", "-version"}, &stdout, io.Discard); err != nil {
		t.Fatalf("run(-version) error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, appName) || !strings.Contains(got, version) {
		t.Errorf("run(-version) printed %q, want the name and version", got)
	}
}

func TestRunHelp(t *testing.T) {
	var stderr bytes.Buffer

	err := run([]string{"imagepullsecret-patcher", "-help"}, io.Discard, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("run(-help) error = %v, want flag.ErrHelp", err)
	}
	if got := stderr.String(); !strings.Contains(got, "-dockerconfigjson") {
		t.Errorf("run(-help) printed %q, want the flag usage", got)
	}
}

func TestRunInvalidConfig(t *testing.T) {
	err := run([]string{"imagepullsecret-patcher", "-dockerconfigjson", "{}", "-dockerconfigjsonpath", "/tmp/creds"}, io.Discard, io.Discard)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		t.Errorf("run() error = %v, want a validation error", err)
	}
}

func TestNewLogger(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      *config.Config
		contains string
	}{
		{
			name:     "text",
			cfg:      &config.Config{LogFormat: config.LogFormatText},
			contains: `msg=hello`,
		},
		{
			name:     "json",
			cfg:      &config.Config{LogFormat: config.LogFormatJSON},
			contains: `"msg":"hello"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			newLogger(tc.cfg, &out).Info("hello")

			if got := out.String(); !strings.Contains(got, tc.contains) {
				t.Errorf("logged %q, want it to contain %q", got, tc.contains)
			}
		})
	}
}

func TestNewLoggerDebug(t *testing.T) {
	ctx := context.Background()

	var quiet, verbose bytes.Buffer
	newLogger(&config.Config{}, &quiet).Log(ctx, slog.LevelDebug, "hello")
	newLogger(&config.Config{Debug: true}, &verbose).Log(ctx, slog.LevelDebug, "hello")

	if quiet.Len() != 0 {
		t.Errorf("debug message logged without -debug: %q", quiet.String())
	}
	if verbose.Len() == 0 {
		t.Error("debug message not logged with -debug")
	}
}

func TestVersionString(t *testing.T) {
	got := versionString("imagepullsecret-patcher")

	for _, want := range []string{"imagepullsecret-patcher", version, "go1."} {
		if !strings.Contains(got, want) {
			t.Errorf("versionString() = %q, want it to contain %q", got, want)
		}
	}
}
