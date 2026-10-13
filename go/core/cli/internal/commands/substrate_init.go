// Copyright 2026 The Kagent Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	podCertificateNamespace = "podcertificate-controller-system"
	podCertificateRelease   = "substrate-podcert"
	poolKeyID               = "1"
	inClusterIssuer         = "https://kubernetes.default.svc"
	caValidity              = 365 * 24 * time.Hour
)

var prepareSubstrate = prepareSubstratePrerequisites

type caKeyType int

const (
	caKeyED25519 caKeyType = iota
	caKeyECDSAP256
)

type serializedCAPool struct {
	CAs              []*serializedCA
	ActiveForSigning string
}

type serializedCA struct {
	ID                 string
	SigningKeyPKCS8    []byte
	RootCertificateDER []byte
}

type serializedJWTPool struct {
	Authorities      []*serializedJWTAuthority
	ActiveForSigning string
}

type serializedJWTAuthority struct {
	ID              string
	Algorithm       string
	SigningKeyPKCS8 []byte
}

func prepareSubstratePrerequisites(ctx context.Context) error {
	restConfig, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	return ensureSubstratePrerequisites(ctx, client)
}

func ensureSubstratePrerequisites(ctx context.Context, client kubernetes.Interface) error {
	if err := ensureNamespace(ctx, client, substrateNamespace, "", ""); err != nil {
		return err
	}
	if err := ensureNamespace(ctx, client, podCertificateNamespace, podCertificateRelease, podCertificateNamespace); err != nil {
		return err
	}
	if err := ensureJWTPool(ctx, client, substrateNamespace, "actor-id-jwt-pool"); err != nil {
		return err
	}
	if err := ensureCAPool(ctx, client, substrateNamespace, "actor-id-ca-pool", caKeyED25519); err != nil {
		return err
	}
	if err := ensureActorIDCACerts(ctx, client); err != nil {
		return err
	}
	if err := ensureCAPool(ctx, client, substrateNamespace, "egress-mitm-ca-pool", caKeyECDSAP256); err != nil {
		return err
	}
	if err := ensureCAPool(ctx, client, podCertificateNamespace, "service-dns-ca-pool", caKeyED25519); err != nil {
		return err
	}
	if err := ensureCAPool(ctx, client, podCertificateNamespace, "pod-identity-ca-pool", caKeyED25519); err != nil {
		return err
	}
	if err := ensureCAPool(ctx, client, podCertificateNamespace, "postgres-ca-pool", caKeyED25519); err != nil {
		return err
	}
	return ensureAPIAuthentication(ctx, client)
}

func ensureNamespace(ctx context.Context, client kubernetes.Interface, name, helmReleaseName, helmReleaseNamespace string) error {
	namespace, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		if helmReleaseName != "" {
			setHelmOwnership(namespace, helmReleaseName, helmReleaseNamespace)
		}
		if _, err := client.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create namespace %s: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get namespace %s: %w", name, err)
	}
	if helmReleaseName == "" || hasHelmOwnership(namespace, helmReleaseName, helmReleaseNamespace) {
		return nil
	}
	setHelmOwnership(namespace, helmReleaseName, helmReleaseNamespace)
	if _, err := client.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("mark namespace %s for Substrate Helm ownership: %w", name, err)
	}
	return nil
}

func hasHelmOwnership(namespace *corev1.Namespace, releaseName, releaseNamespace string) bool {
	return namespace.Labels["app.kubernetes.io/managed-by"] == "Helm" &&
		namespace.Annotations["meta.helm.sh/release-name"] == releaseName &&
		namespace.Annotations["meta.helm.sh/release-namespace"] == releaseNamespace
}

func setHelmOwnership(namespace *corev1.Namespace, releaseName, releaseNamespace string) {
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	if namespace.Annotations == nil {
		namespace.Annotations = map[string]string{}
	}
	namespace.Labels["app.kubernetes.io/managed-by"] = "Helm"
	namespace.Annotations["meta.helm.sh/release-name"] = releaseName
	namespace.Annotations["meta.helm.sh/release-namespace"] = releaseNamespace
}

func ensureCAPool(ctx context.Context, client kubernetes.Interface, namespace, name string, keyType caKeyType) error {
	if exists, err := secretExists(ctx, client, namespace, name); err != nil {
		return err
	} else if exists {
		return nil
	}
	data, err := newCAPoolSecretData(keyType)
	if err != nil {
		return fmt.Errorf("generate CA pool %s/%s: %w", namespace, name, err)
	}
	return createSecret(ctx, client, namespace, name, corev1.SecretTypeTLS, data)
}

func newCAPoolSecretData(keyType caKeyType) (map[string][]byte, error) {
	var privateKey crypto.PrivateKey
	var publicKey crypto.PublicKey
	switch keyType {
	case caKeyED25519:
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		privateKey, publicKey = private, public
	case caKeyECDSAP256:
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		privateKey, publicKey = private, &private.PublicKey
	default:
		return nil, fmt.Errorf("unsupported CA key type %d", keyType)
	}

	now := time.Now()
	template := &x509.Certificate{
		Subject:               pkix.Name{CommonName: rand.Text()},
		NotBefore:             now,
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return nil, err
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	pool, err := json.Marshal(&serializedCAPool{
		CAs: []*serializedCA{{
			ID:                 poolKeyID,
			SigningKeyPKCS8:    privateKeyDER,
			RootCertificateDER: certificateDER,
		}},
		ActiveForSigning: poolKeyID,
	})
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		"pool":                  pool,
		corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}),
	}, nil
}

func ensureJWTPool(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	if exists, err := secretExists(ctx, client, namespace, name); err != nil {
		return err
	} else if exists {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate JWT authority: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode JWT authority: %w", err)
	}
	pool, err := json.Marshal(&serializedJWTPool{
		Authorities:      []*serializedJWTAuthority{{ID: poolKeyID, Algorithm: "ES256", SigningKeyPKCS8: keyDER}},
		ActiveForSigning: poolKeyID,
	})
	if err != nil {
		return fmt.Errorf("encode JWT authority pool: %w", err)
	}
	return createSecret(ctx, client, namespace, name, corev1.SecretTypeOpaque, map[string][]byte{"pool": pool})
}

func ensureActorIDCACerts(ctx context.Context, client kubernetes.Interface) error {
	if exists, err := secretExists(ctx, client, substrateNamespace, "actor-id-ca-certs"); err != nil {
		return err
	} else if exists {
		return nil
	}
	poolSecret, err := client.CoreV1().Secrets(substrateNamespace).Get(ctx, "actor-id-ca-pool", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get actor identity CA pool: %w", err)
	}
	var pool serializedCAPool
	if err := json.Unmarshal(poolSecret.Data["pool"], &pool); err != nil {
		return fmt.Errorf("decode actor identity CA pool: %w", err)
	}
	if len(pool.CAs) == 0 {
		return fmt.Errorf("actor identity CA pool contains no CAs")
	}
	var roots strings.Builder
	for _, ca := range pool.CAs {
		if len(ca.RootCertificateDER) == 0 {
			return fmt.Errorf("CA %q has no root certificate", ca.ID)
		}
		roots.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.RootCertificateDER}))
	}
	return createSecret(ctx, client, substrateNamespace, "actor-id-ca-certs", corev1.SecretTypeOpaque, map[string][]byte{"ca.crt": []byte(roots.String())})
}

func ensureAPIAuthentication(ctx context.Context, client kubernetes.Interface) error {
	if _, err := client.CoreV1().ConfigMaps(substrateNamespace).Get(ctx, "ate-api-authentication", metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get API authentication config: %w", err)
	}
	issuer := clusterOIDCIssuer(ctx, client)
	authentication := fmt.Sprintf("actorIdentityJWTProvider: kubernetes\njwtProviders:\n- name: kubernetes\n  issuer: %s\n  audiences: [api.%s.svc]\n", issuer, substrateNamespace)
	if issuer == inClusterIssuer || issuer == inClusterIssuer+".cluster.local" {
		authentication += "  certificateAuthorityFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt\n" +
			"  discoveryTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token\n"
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: substrateNamespace, Name: "ate-api-authentication"},
		Data:       map[string]string{"authentication.yaml": strings.TrimRight(authentication, "\n")},
	}
	if _, err := client.CoreV1().ConfigMaps(substrateNamespace).Create(ctx, configMap, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create API authentication config: %w", err)
	}
	return nil
}

func clusterOIDCIssuer(ctx context.Context, client kubernetes.Interface) string {
	restClient := client.Discovery().RESTClient()
	if restClient == nil {
		return inClusterIssuer
	}
	raw, err := restClient.Get().AbsPath("/.well-known/openid-configuration").DoRaw(ctx)
	if err != nil {
		return inClusterIssuer
	}
	var document struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Issuer == "" {
		return inClusterIssuer
	}
	return document.Issuer
}

func secretExists(ctx context.Context, client kubernetes.Interface, namespace, name string) (bool, error) {
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
}

func createSecret(ctx context.Context, client kubernetes.Interface, namespace, name string, secretType corev1.SecretType, data map[string][]byte) error {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Type: secretType, Data: data}
	if _, err := client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create secret %s/%s: %w", namespace, name, err)
	}
	return nil
}
