package patcher

import (
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/titansoft-pte-ltd/imagepullsecret-patcher/internal/config"
)

const testDockerConfigJSON = `{"auths":{"gcr.io":{"username":"_json_key","password":"{}"}}}`

func TestVerifySecret(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    *corev1.Secret
		expected verifySecretResult
	}{
		{
			name: "valid",
			input: &corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testDockerConfigJSON)},
			},
			expected: secretOk,
		},
		{
			name: "invalid secret type",
			input: &corev1.Secret{
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testDockerConfigJSON)},
			},
			expected: secretWrongType,
		},
		{
			name: "invalid secret key",
			input: &corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{"test": []byte(testDockerConfigJSON)},
			},
			expected: secretNoKey,
		},
		{
			name: "invalid secret value",
			input: &corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":"invalid"}`)},
			},
			expected: secretDataNotMatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if actual := verifySecret(tc.input, testDockerConfigJSON); actual != tc.expected {
				t.Errorf("verifySecret() = %s, want %s", actual, tc.expected)
			}
		})
	}
}

func TestDockerConfigSecretIsValid(t *testing.T) {
	p := newTestPatcher(t, nil)

	secret := p.dockerConfigSecret(metav1.NamespaceDefault, testDockerConfigJSON)
	if result := verifySecret(secret, testDockerConfigJSON); result != secretOk {
		t.Errorf("dockerConfigSecret() generates an invalid secret: %s", result)
	}
	if !isManagedSecret(secret) {
		t.Error("dockerConfigSecret() generates a secret that is not recognised as managed")
	}
	if secret.Namespace != metav1.NamespaceDefault {
		t.Errorf("Namespace = %q, want %q", secret.Namespace, metav1.NamespaceDefault)
	}
}

func TestIsManagedSecret(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    *corev1.Secret
		expected bool
	}{
		{
			name: "managed by annotation",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{annotationManagedBy: appName},
			}},
			expected: true,
		},
		{
			name: "managed by label",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{labelManagedBy: appName},
			}},
			expected: true,
		},
		{
			name:     "no metadata",
			input:    &corev1.Secret{},
			expected: false,
		},
		{
			name: "different annotation",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"notmatching": "annotation"},
			}},
			expected: false,
		},
		{
			name: "managed by another controller",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{annotationManagedBy: "helm"},
			}},
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if actual := isManagedSecret(tc.input); actual != tc.expected {
				t.Errorf("isManagedSecret() = %t, want %t", actual, tc.expected)
			}
		})
	}
}

func TestDockerConfigJSONFromLiteral(t *testing.T) {
	p := newTestPatcher(t, &config.Config{DockerConfigJSON: testDockerConfigJSON})

	got, err := p.dockerConfigJSON()
	if err != nil {
		t.Fatalf("dockerConfigJSON() error = %v", err)
	}
	if got != testDockerConfigJSON {
		t.Errorf("dockerConfigJSON() = %q, want %q", got, testDockerConfigJSON)
	}
}

func TestDockerConfigJSONFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dockerconfigjson")
	if err := os.WriteFile(path, []byte(testDockerConfigJSON), 0o600); err != nil {
		t.Fatalf("write credential file: %v", err)
	}
	p := newTestPatcher(t, &config.Config{DockerConfigJSONPath: path})

	got, err := p.dockerConfigJSON()
	if err != nil {
		t.Fatalf("dockerConfigJSON() error = %v", err)
	}
	if got != testDockerConfigJSON {
		t.Errorf("dockerConfigJSON() = %q, want %q", got, testDockerConfigJSON)
	}

	// A rotated mounted secret has to be picked up without a restart.
	const rotated = `{"auths":{"gcr.io":{"username":"_json_key","password":"rotated"}}}`
	if err := os.WriteFile(path, []byte(rotated), 0o600); err != nil {
		t.Fatalf("rewrite credential file: %v", err)
	}
	if got, err = p.dockerConfigJSON(); err != nil || got != rotated {
		t.Errorf("dockerConfigJSON() = %q, %v, want %q, nil", got, err, rotated)
	}
}

func TestDockerConfigJSONFromMissingFile(t *testing.T) {
	p := newTestPatcher(t, &config.Config{DockerConfigJSONPath: filepath.Join(t.TempDir(), "absent")})

	if _, err := p.dockerConfigJSON(); err == nil {
		t.Error("dockerConfigJSON() succeeded for a missing file, want an error")
	}
}
