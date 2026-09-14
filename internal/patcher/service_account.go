package patcher

import (
	"encoding/json"
	"slices"

	corev1 "k8s.io/api/core/v1"
)

// serviceAccountPatch is the strategic merge patch applied to a service
// account. imagePullSecrets is a replace-style list, so the patch has to carry
// the entries that already exist.
type serviceAccountPatch struct {
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// hasImagePullSecret reports whether the service account already references
// secretName.
func hasImagePullSecret(sa *corev1.ServiceAccount, secretName string) bool {
	return slices.ContainsFunc(sa.ImagePullSecrets, func(ref corev1.LocalObjectReference) bool {
		return ref.Name == secretName
	})
}

// imagePullSecretPatch returns the patch that appends secretName to the
// service account's existing imagePullSecrets.
func imagePullSecretPatch(sa *corev1.ServiceAccount, secretName string) ([]byte, error) {
	patch := serviceAccountPatch{
		ImagePullSecrets: slices.Clone(sa.ImagePullSecrets),
	}
	if !hasImagePullSecret(sa, secretName) {
		patch.ImagePullSecrets = append(patch.ImagePullSecrets, corev1.LocalObjectReference{Name: secretName})
	}
	return json.Marshal(patch)
}
