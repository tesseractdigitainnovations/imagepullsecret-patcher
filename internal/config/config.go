// Package config parses the imagepullsecret-patcher configuration from
// command-line flags, falling back to environment variables and then to
// built-in defaults.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// DefaultServiceAccountName is the service account patched when neither
// ServiceAccounts nor AllServiceAccount is configured.
const DefaultServiceAccountName = "default"

// Log formats accepted by the -log-format flag.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Config holds the effective runtime configuration.
type Config struct {
	// Force overwrites managed secrets whose content does not match.
	Force bool
	// Debug enables debug-level logging.
	Debug bool
	// LogFormat selects the log handler, either "text" or "json".
	LogFormat string
	// ManagedOnly skips secrets that are not annotated as managed by this app.
	ManagedOnly bool
	// RunOnce performs a single reconciliation and exits.
	RunOnce bool
	// AllServiceAccount patches every service account instead of ServiceAccounts.
	AllServiceAccount bool
	// DockerConfigJSON is the literal credential to distribute.
	DockerConfigJSON string
	// DockerConfigJSONPath is a file holding the credential to distribute.
	DockerConfigJSONPath string
	// SecretName is the name of the secret managed in every namespace.
	SecretName string
	// ExcludedNamespaces are namespaces left untouched.
	ExcludedNamespaces []string
	// ServiceAccounts are the service accounts patched in every namespace.
	ServiceAccounts []string
	// LoopDuration is the interval between reconciliations.
	LoopDuration time.Duration
	// Kubeconfig is an explicit kubeconfig path used when running out of cluster.
	Kubeconfig string
}

// Default returns the configuration used when nothing is set.
func Default() *Config {
	return &Config{
		Force:           true,
		LogFormat:       LogFormatText,
		SecretName:      "image-pull-secret",
		ServiceAccounts: []string{DefaultServiceAccountName},
		LoopDuration:    10 * time.Second,
	}
}

// Parse builds a Config from args, using environment variables as the
// fallback for any flag that is not given. Errors are returned rather than
// exiting so that callers stay testable; output is written to out. It returns
// flag.ErrHelp when -help is requested, and reports true when -version is
// requested.
func Parse(name string, args []string, out io.Writer) (*Config, bool, error) {
	def := Default()
	cfg := &Config{}

	var excludedNamespaces, serviceAccounts string
	var showVersion bool

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.BoolVar(&cfg.Force, "force", LookupEnvOrBool("CONFIG_FORCE", def.Force), "overwrite secrets when the content does not match")
	fs.BoolVar(&cfg.Debug, "debug", LookupEnvOrBool("CONFIG_DEBUG", def.Debug), "show DEBUG logs")
	fs.StringVar(&cfg.LogFormat, "log-format", LookupEnvOrString("CONFIG_LOG_FORMAT", def.LogFormat), `log output format, either "text" or "json"`)
	fs.BoolVar(&cfg.ManagedOnly, "managedonly", LookupEnvOrBool("CONFIG_MANAGEDONLY", def.ManagedOnly), "only modify secrets which are annotated as managed by imagepullsecret-patcher")
	fs.BoolVar(&cfg.RunOnce, "runonce", LookupEnvOrBool("CONFIG_RUNONCE", def.RunOnce), "run a single update and exit instead of looping")
	fs.BoolVar(&cfg.AllServiceAccount, "allserviceaccount", LookupEnvOrBool("CONFIG_ALLSERVICEACCOUNT", def.AllServiceAccount), "if false, patch just the service accounts named by -serviceaccounts; if true, list and patch all service accounts")
	fs.StringVar(&cfg.DockerConfigJSON, "dockerconfigjson", LookupEnvOrString("CONFIG_DOCKERCONFIGJSON", def.DockerConfigJSON), "json credential for authenticating the container registry, exclusive with -dockerconfigjsonpath")
	fs.StringVar(&cfg.DockerConfigJSONPath, "dockerconfigjsonpath", LookupEnvOrString("CONFIG_DOCKERCONFIGJSONPATH", def.DockerConfigJSONPath), "path to a json file containing the credential to be distributed, exclusive with -dockerconfigjson")
	fs.StringVar(&cfg.SecretName, "secretname", LookupEnvOrString("CONFIG_SECRETNAME", def.SecretName), "name of the managed secrets")
	fs.StringVar(&excludedNamespaces, "excluded-namespaces", LookupEnvOrString("CONFIG_EXCLUDED_NAMESPACES", strings.Join(def.ExcludedNamespaces, ",")), "comma-separated namespaces excluded from processing")
	fs.StringVar(&serviceAccounts, "serviceaccounts", LookupEnvOrString("CONFIG_SERVICEACCOUNTS", strings.Join(def.ServiceAccounts, ",")), "comma-separated list of service accounts to patch")
	fs.DurationVar(&cfg.LoopDuration, "loop-duration", LookupEnvOrDuration("CONFIG_LOOP_DURATION", def.LoopDuration), "duration between reconciliation loops")
	fs.StringVar(&cfg.Kubeconfig, "kubeconfig", LookupEnvOrString("KUBECONFIG", def.Kubeconfig), "path to a kubeconfig file, used only when running outside a cluster")
	fs.BoolVar(&showVersion, "version", false, "print version information and exit")

	if err := fs.Parse(args); err != nil {
		return nil, false, err
	}
	if showVersion {
		return nil, true, nil
	}
	if extra := fs.Args(); len(extra) > 0 {
		return nil, false, fmt.Errorf("unexpected positional arguments: %s", strings.Join(extra, " "))
	}

	cfg.ExcludedNamespaces = splitList(excludedNamespaces)
	cfg.ServiceAccounts = splitList(serviceAccounts)

	if err := cfg.Validate(); err != nil {
		return nil, false, err
	}
	return cfg, false, nil
}

// Validate reports whether the configuration is internally consistent.
func (c *Config) Validate() error {
	if c.DockerConfigJSON != "" && c.DockerConfigJSONPath != "" {
		return errors.New("cannot specify both -dockerconfigjson and -dockerconfigjsonpath")
	}
	if c.SecretName == "" {
		return errors.New("-secretname must not be empty")
	}
	if c.LoopDuration <= 0 && !c.RunOnce {
		return fmt.Errorf("-loop-duration must be positive, got %s", c.LoopDuration)
	}
	if !c.AllServiceAccount && len(c.ServiceAccounts) == 0 {
		return errors.New("-serviceaccounts must not be empty unless -allserviceaccount is set")
	}
	switch c.LogFormat {
	case LogFormatText, LogFormatJSON:
	default:
		return fmt.Errorf("-log-format must be %q or %q, got %q", LogFormatText, LogFormatJSON, c.LogFormat)
	}
	return nil
}

// NamespaceExcluded reports whether the named namespace is listed in
// ExcludedNamespaces.
func (c *Config) NamespaceExcluded(namespace string) bool {
	return slices.Contains(c.ExcludedNamespaces, namespace)
}

// ServiceAccountSelected reports whether the named service account should be
// patched.
func (c *Config) ServiceAccountSelected(name string) bool {
	return c.AllServiceAccount || slices.Contains(c.ServiceAccounts, name)
}

// splitList splits a comma-separated flag value, dropping empty entries and
// surrounding whitespace.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
