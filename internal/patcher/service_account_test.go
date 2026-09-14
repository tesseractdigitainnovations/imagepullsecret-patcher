package patcher

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestHasImagePullSecret(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sa         *corev1.ServiceAccount
		secretName string
		expected   bool
	}{
		{
			name:       "positive one secret",
			sa:         serviceAccountWith("secret-a"),
			secretName: "secret-a",
			expected:   true,
		},
		{
			name:       "positive two secrets",
			sa:         serviceAccountWith("secret-a", "secret-b"),
			secretName: "secret-a",
			expected:   true,
		},
		{
			name:       "negative no secret",
			sa:         serviceAccountWith(),
			secretName: "secret-a",
			expected:   false,
		},
		{
			name:       "negative one secret",
			sa:         serviceAccountWith("secret-b"),
			secretName: "secret-a",
			expected:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if actual := hasImagePullSecret(tc.sa, tc.secretName); actual != tc.expected {
				t.Errorf("hasImagePullSecret() = %v, want %v", actual, tc.expected)
			}
		})
	}
}

func TestImagePullSecretPatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sa         *corev1.ServiceAccount
		secretName string
		expected   string
	}{
		{
			name:       "empty",
			sa:         serviceAccountWith(),
			secretName: "secret-a",
			expected:   `{"imagePullSecrets":[{"name":"secret-a"}]}`,
		},
		{
			name:       "same",
			sa:         serviceAccountWith("secret-a"),
			secretName: "secret-a",
			expected:   `{"imagePullSecrets":[{"name":"secret-a"}]}`,
		},
		{
			name:       "different",
			sa:         serviceAccountWith("secret-b"),
			secretName: "secret-a",
			expected:   `{"imagePullSecrets":[{"name":"secret-b"},{"name":"secret-a"}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := imagePullSecretPatch(tc.sa, tc.secretName)
			if err != nil {
				t.Fatalf("imagePullSecretPatch() error = %v", err)
			}
			if string(actual) != tc.expected {
				t.Errorf("imagePullSecretPatch() = %s, want %s", actual, tc.expected)
			}
		})
	}
}

// TestImagePullSecretPatchDoesNotMutate guards the copy taken by the patch
// builder: mutating the caller's service account would corrupt the list that a
// later comparison relies on.
func TestImagePullSecretPatchDoesNotMutate(t *testing.T) {
	sa := serviceAccountWith("secret-b")
	if _, err := imagePullSecretPatch(sa, "secret-a"); err != nil {
		t.Fatalf("imagePullSecretPatch() error = %v", err)
	}
	if len(sa.ImagePullSecrets) != 1 {
		t.Errorf("imagePullSecretPatch() mutated the service account: %v", sa.ImagePullSecrets)
	}
}

func serviceAccountWith(secretNames ...string) *corev1.ServiceAccount {
	refs := make([]corev1.LocalObjectReference, 0, len(secretNames))
	for _, name := range secretNames {
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return &corev1.ServiceAccount{ImagePullSecrets: refs}
}
