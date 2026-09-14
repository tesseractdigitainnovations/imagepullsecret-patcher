// Package patcher reconciles an image pull secret into every Kubernetes
// namespace and patches it onto the selected service accounts.
package patcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/config"
)

// AnnotationExclude marks a namespace as excluded from processing when set to
// "true".
const AnnotationExclude = "k8s.titansoft.com/imagepullsecret-patcher-exclude"

// Patcher reconciles the managed secret and service accounts of a cluster.
type Patcher struct {
	client kubernetes.Interface
	cfg    *config.Config
	log    *slog.Logger
}

// New returns a Patcher using the given client and configuration.
func New(client kubernetes.Interface, cfg *config.Config, logger *slog.Logger) *Patcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Patcher{client: client, cfg: cfg, log: logger}
}

// Run reconciles once when the configuration asks for a single run, otherwise
// it reconciles every LoopDuration until ctx is cancelled. Errors from an
// individual loop are logged and retried on the next tick; only a failed
// single run is returned to the caller.
func (p *Patcher) Run(ctx context.Context) error {
	if p.cfg.RunOnce {
		p.log.Info("running a single reconciliation per -runonce")
		return p.Reconcile(ctx)
	}

	ticker := time.NewTicker(p.cfg.LoopDuration)
	defer ticker.Stop()

loop:
	for {
		if err := p.Reconcile(ctx); err != nil {
			if ctx.Err() != nil {
				break loop
			}
			p.log.Error("reconciliation failed, retrying next loop", "error", err)
		}

		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
		}
	}

	p.log.Info("shutting down")
	return nil
}

// Reconcile makes sure the managed secret exists in every namespace and is
// referenced by the selected service accounts. It processes every namespace
// even when some of them fail, and returns the accumulated errors.
func (p *Patcher) Reconcile(ctx context.Context) error {
	dockerConfigJSON, err := p.dockerConfigJSON()
	if err != nil {
		return fmt.Errorf("resolve dockerconfigjson: %w", err)
	}

	namespaces, err := p.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}
	p.log.Debug("listed namespaces", "count", len(namespaces.Items))

	var errs []error
	for _, ns := range namespaces.Items {
		if err := ctx.Err(); err != nil {
			return err
		}

		log := p.log.With("namespace", ns.Name)
		if p.namespaceExcluded(ns) {
			log.Debug("namespace skipped")
			continue
		}

		// A namespace without a valid secret must not have its service
		// accounts patched, as that would point them at a missing secret.
		if err := p.processSecret(ctx, log, ns.Name, dockerConfigJSON); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := p.processServiceAccounts(ctx, log, ns.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// namespaceExcluded reports whether a namespace opted out through its
// annotation or is listed in the configuration.
func (p *Patcher) namespaceExcluded(ns corev1.Namespace) bool {
	if ns.Annotations[AnnotationExclude] == "true" {
		return true
	}
	return p.cfg.NamespaceExcluded(ns.Name)
}

// processSecret creates, updates or recreates the managed secret of a
// namespace so that it holds the desired credential.
func (p *Patcher) processSecret(ctx context.Context, log *slog.Logger, namespace, dockerConfigJSON string) error {
	secrets := p.client.CoreV1().Secrets(namespace)

	secret, err := secrets.Get(ctx, p.cfg.SecretName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		if _, err := secrets.Create(ctx, p.dockerConfigSecret(namespace, dockerConfigJSON), metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Lost a race with another writer; the next loop reconciles it.
				log.Debug("secret created concurrently", "secret", p.cfg.SecretName)
				return nil
			}
			return fmt.Errorf("[%s] create secret %s: %w", namespace, p.cfg.SecretName, err)
		}
		log.Info("created secret", "secret", p.cfg.SecretName)
		return nil
	case err != nil:
		return fmt.Errorf("[%s] get secret %s: %w", namespace, p.cfg.SecretName, err)
	}

	if p.cfg.ManagedOnly && !isManagedSecret(secret) {
		return fmt.Errorf("[%s] secret %s is present but not managed by %s", namespace, p.cfg.SecretName, appName)
	}

	result := verifySecret(secret, dockerConfigJSON)
	if result == secretOk {
		log.Debug("secret is valid", "secret", p.cfg.SecretName)
		return nil
	}
	if !p.cfg.Force {
		return fmt.Errorf("[%s] secret %s is not valid (%s), set -force to overwrite", namespace, p.cfg.SecretName, result)
	}
	log.Warn("secret is not valid, overwriting", "secret", p.cfg.SecretName, "reason", result)

	// The type of a secret is immutable, so a secret of the wrong type has to
	// be replaced. Everything else can be updated in place, which avoids a
	// window where the secret does not exist.
	if result == secretWrongType {
		if err := secrets.Delete(ctx, p.cfg.SecretName, metav1.DeleteOptions{
			Preconditions: metav1.NewUIDPreconditions(string(secret.UID)),
		}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("[%s] delete secret %s: %w", namespace, p.cfg.SecretName, err)
		}
		if _, err := secrets.Create(ctx, p.dockerConfigSecret(namespace, dockerConfigJSON), metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("[%s] recreate secret %s: %w", namespace, p.cfg.SecretName, err)
		}
		log.Info("recreated secret", "secret", p.cfg.SecretName)
		return nil
	}

	desired := secret.DeepCopy()
	desired.Data = map[string][]byte{corev1.DockerConfigJsonKey: []byte(dockerConfigJSON)}
	if desired.Labels == nil {
		desired.Labels = map[string]string{}
	}
	desired.Labels[labelManagedBy] = appName
	if desired.Annotations == nil {
		desired.Annotations = map[string]string{}
	}
	desired.Annotations[annotationManagedBy] = appName
	if _, err := secrets.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("[%s] update secret %s: %w", namespace, p.cfg.SecretName, err)
	}
	log.Info("updated secret", "secret", p.cfg.SecretName)
	return nil
}

// processServiceAccounts patches the managed secret onto the selected service
// accounts of a namespace.
func (p *Patcher) processServiceAccounts(ctx context.Context, log *slog.Logger, namespace string) error {
	serviceAccounts := p.client.CoreV1().ServiceAccounts(namespace)

	list, err := serviceAccounts.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("[%s] list service accounts: %w", namespace, err)
	}

	var errs []error
	for _, sa := range list.Items {
		if !p.cfg.ServiceAccountSelected(sa.Name) {
			log.Debug("service account skipped", "serviceaccount", sa.Name)
			continue
		}
		if hasImagePullSecret(&sa, p.cfg.SecretName) {
			log.Debug("service account already references the secret", "serviceaccount", sa.Name)
			continue
		}

		patch, err := imagePullSecretPatch(&sa, p.cfg.SecretName)
		if err != nil {
			errs = append(errs, fmt.Errorf("[%s] build patch for service account %s: %w", namespace, sa.Name, err))
			continue
		}
		if _, err := serviceAccounts.Patch(ctx, sa.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
			errs = append(errs, fmt.Errorf("[%s] patch imagePullSecrets onto service account %s: %w", namespace, sa.Name, err))
			continue
		}
		log.Info("patched imagePullSecrets onto service account", "serviceaccount", sa.Name, "secret", p.cfg.SecretName)
	}
	return errors.Join(errs...)
}
