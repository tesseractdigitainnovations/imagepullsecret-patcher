package patcher

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Annotations and labels applied to the secrets managed by this application.
const (
	labelManagedBy = "app.kubernetes.io/managed-by"
	// annotationManagedBy is kept for backwards compatibility with secrets
	// created by older releases, which only set the annotation.
	annotationManagedBy = "app.kubernetes.io/managed-by"

	appName = "imagepullsecret-patcher"
)

// verifySecretResult describes how an existing secret differs from the desired
// one.
type verifySecretResult string

const (
	secretOk           verifySecretResult = "SecretOk"
	secretWrongType    verifySecretResult = "SecretWrongType"
	secretNoKey        verifySecretResult = "SecretNoKey"
	secretDataNotMatch verifySecretResult = "SecretDataNotMatch"
)

// dockerConfigJSON resolves the credential to distribute, reading it from disk
// on every call so that a mounted secret can be rotated without a restart.
func (p *Patcher) dockerConfigJSON() (string, error) {
	if p.cfg.DockerConfigJSONPath == "" {
		return p.cfg.DockerConfigJSON, nil
	}
	b, err := os.ReadFile(p.cfg.DockerConfigJSONPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.cfg.DockerConfigJSONPath, err)
	}
	return string(b), nil
}

// dockerConfigSecret builds the desired secret for a namespace.
func (p *Patcher) dockerConfigSecret(namespace, dockerConfigJSON string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.cfg.SecretName,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: appName,
			},
			Annotations: map[string]string{
				annotationManagedBy: appName,
			},
		},
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		},
		Type: corev1.SecretTypeDockerConfigJson,
	}
}

// verifySecret compares an existing secret against the desired credential.
func verifySecret(secret *corev1.Secret, dockerConfigJSON string) verifySecretResult {
	if secret.Type != corev1.SecretTypeDockerConfigJson {
		return secretWrongType
	}
	b, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return secretNoKey
	}
	if string(b) != dockerConfigJSON {
		return secretDataNotMatch
	}
	return secretOk
}

// isManagedSecret reports whether the secret was created by this application.
func isManagedSecret(secret *corev1.Secret) bool {
	return secret.Labels[labelManagedBy] == appName ||
		secret.Annotations[annotationManagedBy] == appName
}
