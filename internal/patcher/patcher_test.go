package patcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/config"
)

const testSecretName = "image-pull-secret"

// newTestPatcher returns a Patcher backed by a fake clientset. Fields left
// unset on cfg fall back to the defaults, and logging is discarded.
func newTestPatcher(t *testing.T, cfg *config.Config, objects ...runtime.Object) *Patcher {
	t.Helper()

	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.SecretName == "" {
		cfg.SecretName = testSecretName
	}
	if cfg.DockerConfigJSON == "" && cfg.DockerConfigJSONPath == "" {
		cfg.DockerConfigJSON = testDockerConfigJSON
	}
	if len(cfg.ServiceAccounts) == 0 {
		cfg.ServiceAccounts = []string{config.DefaultServiceAccountName}
	}
	if cfg.LoopDuration == 0 {
		cfg.LoopDuration = time.Millisecond
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(fake.NewClientset(objects...), cfg, logger)
}

func TestProcessSecret(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		// existing is the secret already present in the namespace, if any.
		existing *corev1.Secret
		wantErr  bool
		// wantValid asserts the state of the secret after processing.
		wantValid bool
	}{
		{
			name:      "no secret is created",
			wantValid: true,
		},
		{
			name:      "valid secret is left alone",
			existing:  validSecret(),
			wantValid: true,
		},
		{
			name:      "stale secret is updated when forced",
			cfg:       &config.Config{Force: true},
			existing:  staleSecret(),
			wantValid: true,
		},
		{
			name:      "stale secret is kept when not forced",
			cfg:       &config.Config{Force: false},
			existing:  staleSecret(),
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "wrong type is recreated when forced",
			cfg:       &config.Config{Force: true},
			existing:  opaqueSecret(),
			wantValid: true,
		},
		{
			name:      "wrong type is kept when not forced",
			cfg:       &config.Config{Force: false},
			existing:  opaqueSecret(),
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "unmanaged secret is skipped with managedonly",
			cfg:       &config.Config{Force: true, ManagedOnly: true},
			existing:  unmanagedStaleSecret(),
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "managed secret is updated with managedonly",
			cfg:       &config.Config{Force: true, ManagedOnly: true},
			existing:  staleSecret(),
			wantValid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var objects []runtime.Object
			if tc.existing != nil {
				objects = append(objects, tc.existing)
			}
			p := newTestPatcher(t, tc.cfg, objects...)

			err := p.processSecret(context.Background(), p.log, metav1.NamespaceDefault, testDockerConfigJSON)
			if (err != nil) != tc.wantErr {
				t.Fatalf("processSecret() error = %v, wantErr %v", err, tc.wantErr)
			}

			secret, err := p.client.CoreV1().Secrets(metav1.NamespaceDefault).Get(context.Background(), testSecretName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get secret after processing: %v", err)
			}
			if valid := verifySecret(secret, testDockerConfigJSON) == secretOk; valid != tc.wantValid {
				t.Errorf("secret valid = %v, want %v", valid, tc.wantValid)
			}
		})
	}
}

// TestProcessSecretUpdateKeepsIdentity checks that a stale secret is updated in
// place rather than deleted and recreated, so that the secret never disappears
// from under a running pod.
func TestProcessSecretUpdateKeepsIdentity(t *testing.T) {
	existing := staleSecret()
	existing.UID = "keep-me"
	p := newTestPatcher(t, &config.Config{Force: true}, existing)

	if err := p.processSecret(context.Background(), p.log, metav1.NamespaceDefault, testDockerConfigJSON); err != nil {
		t.Fatalf("processSecret() error = %v", err)
	}

	secret, err := p.client.CoreV1().Secrets(metav1.NamespaceDefault).Get(context.Background(), testSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret after processing: %v", err)
	}
	if secret.UID != "keep-me" {
		t.Errorf("UID = %q, want the secret to be updated in place", secret.UID)
	}
	if !isManagedSecret(secret) {
		t.Error("updated secret is not marked as managed")
	}
}

func TestProcessSecretGetError(t *testing.T) {
	p := newTestPatcher(t, nil)
	client := p.client.(*fake.Clientset)
	client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})

	if err := p.processSecret(context.Background(), p.log, metav1.NamespaceDefault, testDockerConfigJSON); err == nil {
		t.Error("processSecret() succeeded despite an API error, want an error")
	}
}

func TestProcessServiceAccounts(t *testing.T) {
	for _, tc := range []struct {
		name               string
		cfg                *config.Config
		serviceAccounts    []runtime.Object
		wantPatched        []string
		wantNotPatched     []string
		wantKeepsExistingA bool
	}{
		{
			name:            "service account without image pull secret is patched",
			serviceAccounts: []runtime.Object{serviceAccount(config.DefaultServiceAccountName)},
			wantPatched:     []string{config.DefaultServiceAccountName},
		},
		{
			name:            "service account with the secret is left alone",
			serviceAccounts: []runtime.Object{serviceAccount(config.DefaultServiceAccountName, testSecretName)},
			wantPatched:     []string{config.DefaultServiceAccountName},
		},
		{
			name:               "existing image pull secrets are preserved",
			serviceAccounts:    []runtime.Object{serviceAccount(config.DefaultServiceAccountName, "other-secret")},
			wantPatched:        []string{config.DefaultServiceAccountName},
			wantKeepsExistingA: true,
		},
		{
			name:            "unselected service account is skipped",
			serviceAccounts: []runtime.Object{serviceAccount("other-service-account")},
			wantNotPatched:  []string{"other-service-account"},
		},
		{
			name:            "all service accounts are patched with allserviceaccount",
			cfg:             &config.Config{AllServiceAccount: true},
			serviceAccounts: []runtime.Object{serviceAccount(config.DefaultServiceAccountName), serviceAccount("other-service-account")},
			wantPatched:     []string{config.DefaultServiceAccountName, "other-service-account"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPatcher(t, tc.cfg, tc.serviceAccounts...)

			if err := p.processServiceAccounts(context.Background(), p.log, metav1.NamespaceDefault); err != nil {
				t.Fatalf("processServiceAccounts() error = %v", err)
			}

			for _, name := range tc.wantPatched {
				if !serviceAccountHasSecret(t, p, name, testSecretName) {
					t.Errorf("service account %q does not reference %q", name, testSecretName)
				}
			}
			for _, name := range tc.wantNotPatched {
				if serviceAccountHasSecret(t, p, name, testSecretName) {
					t.Errorf("service account %q references %q but should have been skipped", name, testSecretName)
				}
			}
			if tc.wantKeepsExistingA && !serviceAccountHasSecret(t, p, config.DefaultServiceAccountName, "other-secret") {
				t.Error("patching dropped the pre-existing image pull secret")
			}
		})
	}
}

func TestProcessServiceAccountsListError(t *testing.T) {
	p := newTestPatcher(t, nil)
	client := p.client.(*fake.Clientset)
	client.PrependReactor("list", "serviceaccounts", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})

	if err := p.processServiceAccounts(context.Background(), p.log, metav1.NamespaceDefault); err == nil {
		t.Error("processServiceAccounts() succeeded despite an API error, want an error")
	}
}

func TestReconcile(t *testing.T) {
	p := newTestPatcher(t, &config.Config{ExcludedNamespaces: []string{"excluded-by-config"}},
		namespace("default", nil),
		namespace("other", nil),
		namespace("excluded-by-config", nil),
		namespace("excluded-by-annotation", map[string]string{AnnotationExclude: "true"}),
		namespace("annotated-false", map[string]string{AnnotationExclude: "false"}),
		serviceAccountIn("default", config.DefaultServiceAccountName),
		serviceAccountIn("other", config.DefaultServiceAccountName),
	)

	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	for _, ns := range []string{"default", "other", "annotated-false"} {
		secret, err := p.client.CoreV1().Secrets(ns).Get(context.Background(), testSecretName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("namespace %q has no managed secret: %v", ns, err)
			continue
		}
		if result := verifySecret(secret, testDockerConfigJSON); result != secretOk {
			t.Errorf("namespace %q has an invalid secret: %s", ns, result)
		}
	}
	for _, ns := range []string{"excluded-by-config", "excluded-by-annotation"} {
		_, err := p.client.CoreV1().Secrets(ns).Get(context.Background(), testSecretName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("excluded namespace %q got a secret (err = %v)", ns, err)
		}
	}
	if !serviceAccountHasSecret(t, p, config.DefaultServiceAccountName, testSecretName) {
		t.Error("service account in the default namespace was not patched")
	}
}

// TestReconcileContinuesAfterNamespaceError checks that one broken namespace
// does not stop the others from being reconciled.
func TestReconcileContinuesAfterNamespaceError(t *testing.T) {
	p := newTestPatcher(t, nil, namespace("broken", nil), namespace("healthy", nil))
	client := p.client.(*fake.Clientset)
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() == "broken" {
			return true, nil, errors.New("boom")
		}
		return false, nil, nil
	})

	err := p.Reconcile(context.Background())
	if err == nil {
		t.Error("Reconcile() succeeded despite a failing namespace, want an error")
	}
	if _, err := p.client.CoreV1().Secrets("healthy").Get(context.Background(), testSecretName, metav1.GetOptions{}); err != nil {
		t.Errorf("healthy namespace was not reconciled: %v", err)
	}
}

func TestReconcileListNamespacesError(t *testing.T) {
	p := newTestPatcher(t, nil)
	client := p.client.(*fake.Clientset)
	client.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})

	if err := p.Reconcile(context.Background()); err == nil {
		t.Error("Reconcile() succeeded despite a failing namespace list, want an error")
	}
}

func TestReconcileMissingCredentialFile(t *testing.T) {
	p := newTestPatcher(t, &config.Config{DockerConfigJSONPath: "/nonexistent/.dockerconfigjson"})

	if err := p.Reconcile(context.Background()); err == nil {
		t.Error("Reconcile() succeeded with an unreadable credential, want an error")
	}
}

func TestRunOnce(t *testing.T) {
	p := newTestPatcher(t, &config.Config{RunOnce: true}, namespace("default", nil))

	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := p.client.CoreV1().Secrets("default").Get(context.Background(), testSecretName, metav1.GetOptions{}); err != nil {
		t.Errorf("single run did not create the secret: %v", err)
	}
}

// TestRunOnceReturnsError checks that a failing single run is reported to the
// caller, so that a CronJob pod ends up in a failed state.
func TestRunOnceReturnsError(t *testing.T) {
	p := newTestPatcher(t, &config.Config{RunOnce: true})
	client := p.client.(*fake.Clientset)
	client.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})

	if err := p.Run(context.Background()); err == nil {
		t.Error("Run() with -runonce succeeded despite an API error, want an error")
	}
}

// TestRunStopsOnContextCancel checks the graceful shutdown path of the loop.
func TestRunStopsOnContextCancel(t *testing.T) {
	p := newTestPatcher(t, nil, namespace("default", nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Wait for the first loop to do its work, then ask the loop to stop.
	deadline := time.After(5 * time.Second)
	for {
		_, err := p.client.CoreV1().Secrets("default").Get(context.Background(), testSecretName, metav1.GetOptions{})
		if err == nil {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("the first loop did not create the secret in time")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Run() did not return after the context was cancelled")
	}
}

func TestNamespaceExcluded(t *testing.T) {
	p := newTestPatcher(t, &config.Config{ExcludedNamespaces: []string{"kube-system", "other-namespace"}})

	for _, tc := range []struct {
		name      string
		namespace corev1.Namespace
		expected  bool
	}{
		{
			name:      "not excluded",
			namespace: *namespace("default", nil),
			expected:  false,
		},
		{
			name:      "listed in the configuration",
			namespace: *namespace("kube-system", nil),
			expected:  true,
		},
		{
			name:      "annotated true",
			namespace: *namespace("default", map[string]string{AnnotationExclude: "true"}),
			expected:  true,
		},
		{
			name:      "annotated false",
			namespace: *namespace("default", map[string]string{AnnotationExclude: "false"}),
			expected:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if actual := p.namespaceExcluded(tc.namespace); actual != tc.expected {
				t.Errorf("namespaceExcluded() = %v, want %v", actual, tc.expected)
			}
		})
	}
}

// test fixtures

func namespace(name string, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:        name,
		Annotations: annotations,
	}}
}

func serviceAccount(name string, imagePullSecrets ...string) *corev1.ServiceAccount {
	sa := serviceAccountWith(imagePullSecrets...)
	sa.Name = name
	sa.Namespace = metav1.NamespaceDefault
	return sa
}

func serviceAccountIn(namespace, name string) *corev1.ServiceAccount {
	sa := serviceAccount(name)
	sa.Namespace = namespace
	return sa
}

func validSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testSecretName,
			Namespace:   metav1.NamespaceDefault,
			Labels:      map[string]string{labelManagedBy: appName},
			Annotations: map[string]string{annotationManagedBy: appName},
		},
		Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testDockerConfigJSON)},
		Type: corev1.SecretTypeDockerConfigJson,
	}
}

func staleSecret() *corev1.Secret {
	secret := validSecret()
	secret.Data = map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}
	return secret
}

func unmanagedStaleSecret() *corev1.Secret {
	secret := staleSecret()
	secret.Labels = nil
	secret.Annotations = nil
	return secret
}

func opaqueSecret() *corev1.Secret {
	secret := validSecret()
	secret.Type = corev1.SecretTypeOpaque
	return secret
}

func serviceAccountHasSecret(t *testing.T, p *Patcher, serviceAccountName, secretName string) bool {
	t.Helper()

	sa, err := p.client.CoreV1().ServiceAccounts(metav1.NamespaceDefault).Get(context.Background(), serviceAccountName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service account %q: %v", serviceAccountName, err)
	}
	return hasImagePullSecret(sa, secretName)
}
