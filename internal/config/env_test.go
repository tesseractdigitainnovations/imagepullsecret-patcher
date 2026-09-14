package config

import (
	"testing"
	"time"
)

func TestLookupEnvOrString(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        map[string]string
		lookupKey  string
		defaultVal string
		expected   string
	}{
		{
			name:       "hit",
			env:        map[string]string{"TEST": "test"},
			lookupKey:  "TEST",
			defaultVal: "default",
			expected:   "test",
		},
		{
			name:       "miss",
			env:        map[string]string{"MISS": "miss"},
			lookupKey:  "TEST",
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "empty value wins over default",
			env:        map[string]string{"TEST": ""},
			lookupKey:  "TEST",
			defaultVal: "default",
			expected:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			if actual := LookupEnvOrString(tc.lookupKey, tc.defaultVal); actual != tc.expected {
				t.Errorf("LookupEnvOrString(%q, %q) = %q, want %q", tc.lookupKey, tc.defaultVal, actual, tc.expected)
			}
		})
	}
}

func TestLookupEnvOrBool(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        map[string]string
		lookupKey  string
		defaultVal bool
		expected   bool
	}{
		{
			name:       "hit",
			env:        map[string]string{"TEST": "true"},
			lookupKey:  "TEST",
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "miss",
			env:        map[string]string{"MISS": "true"},
			lookupKey:  "TEST",
			defaultVal: false,
			expected:   false,
		},
		{
			name:       "unparsable falls back to the default",
			env:        map[string]string{"TEST": "notabool"},
			lookupKey:  "TEST",
			defaultVal: true,
			expected:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			if actual := LookupEnvOrBool(tc.lookupKey, tc.defaultVal); actual != tc.expected {
				t.Errorf("LookupEnvOrBool(%q, %v) = %v, want %v", tc.lookupKey, tc.defaultVal, actual, tc.expected)
			}
		})
	}
}

func TestLookupEnvOrDuration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        map[string]string
		lookupKey  string
		defaultVal time.Duration
		expected   time.Duration
	}{
		{
			name:       "hit",
			env:        map[string]string{"TEST": "1m30s"},
			lookupKey:  "TEST",
			defaultVal: 10 * time.Second,
			expected:   90 * time.Second,
		},
		{
			name:       "miss",
			env:        map[string]string{"MISS": "1m"},
			lookupKey:  "TEST",
			defaultVal: 10 * time.Second,
			expected:   10 * time.Second,
		},
		{
			name:       "unparsable falls back to the default",
			env:        map[string]string{"TEST": "ten seconds"},
			lookupKey:  "TEST",
			defaultVal: 10 * time.Second,
			expected:   10 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			if actual := LookupEnvOrDuration(tc.lookupKey, tc.defaultVal); actual != tc.expected {
				t.Errorf("LookupEnvOrDuration(%q, %s) = %s, want %s", tc.lookupKey, tc.defaultVal, actual, tc.expected)
			}
		})
	}
}

// setEnv sets the given environment variables for the duration of the test.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}
