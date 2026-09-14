// Command imagepullsecret-patcher creates an image pull secret in every
// Kubernetes namespace and patches it onto the selected service accounts, so
// that a cluster has authenticated access to a private container registry.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/config"
	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/patcher"
)

const appName = "imagepullsecret-patcher"

// Overridden at build time with -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	if err := run(os.Args, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	name := filepath.Base(args[0])

	cfg, showVersion, err := config.Parse(name, args[1:], stderr)
	if err != nil {
		return err
	}
	if showVersion {
		fmt.Fprintln(stdout, versionString(name))
		return nil
	}

	logger := newLogger(cfg, stderr)
	slog.SetDefault(logger)

	if cfg.DockerConfigJSON == "" && cfg.DockerConfigJSONPath == "" {
		logger.Warn("neither -dockerconfigjson nor -dockerconfigjsonpath is set, the managed secret will hold an empty credential")
	}

	// Stop reconciling on SIGINT or SIGTERM so that a rolling update or a
	// `kubectl delete pod` does not interrupt an in-flight API call.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	restConfig, err := restConfig(cfg.Kubeconfig)
	if err != nil {
		return err
	}
	restConfig.UserAgent = fmt.Sprintf("%s/%s", appName, version)

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	logger.Info("application started",
		"version", version,
		"secret", cfg.SecretName,
		"runonce", cfg.RunOnce,
		"loop_duration", cfg.LoopDuration.String(),
		"all_service_accounts", cfg.AllServiceAccount,
	)
	return patcher.New(clientset, cfg, logger).Run(ctx)
}

// newLogger builds the structured logger described by the configuration.
func newLogger(cfg *config.Config, out io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.Debug {
		opts.Level = slog.LevelDebug
	}

	var handler slog.Handler
	if cfg.LogFormat == config.LogFormatJSON {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}
	return slog.New(handler)
}

// restConfig returns the in-cluster configuration, falling back to a kubeconfig
// file so that the patcher can also be run from a workstation.
func restConfig(kubeconfig string) (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, rest.ErrNotInCluster) {
		return nil, fmt.Errorf("load in-cluster config: %w", err)
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig, and not running in a cluster: %w", err)
	}
	return cfg, nil
}

// versionString reports the build information, falling back to the data the Go
// toolchain stamps into the binary when no ldflags were given.
func versionString(name string) string {
	v, c, d := version, commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if c == "" {
					c = setting.Value
				}
			case "vcs.time":
				if d == "" {
					d = setting.Value
				}
			}
		}
	}

	s := fmt.Sprintf("%s %s", name, v)
	if c != "" {
		s += fmt.Sprintf(" (%s)", c)
	}
	if d != "" {
		s += fmt.Sprintf(" built %s", d)
	}
	return s + fmt.Sprintf(" %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
