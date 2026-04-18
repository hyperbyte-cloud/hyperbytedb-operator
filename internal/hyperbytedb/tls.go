package hyperbytedb

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func TLSSecretName(cluster *v1alpha1.HyperbytedbCluster) string {
	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.SecretName != "" {
		return cluster.Spec.Server.TLS.SecretName
	}
	return cluster.Name + "-tls"
}

// BuildSelfSignedTLSSecret generates a CA + server certificate bundle as a
// Kubernetes TLS Secret. The certificate covers the headless service DNS
// entries for every pod ordinal up to maxOrdinal.
func BuildSelfSignedTLSSecret(
	cluster *v1alpha1.HyperbytedbCluster,
	namespace string,
	replicas int32,
) (*corev1.Secret, error) {
	stsName := StatefulSetName(cluster)
	headlessSvc := HeadlessServiceName(cluster)

	// SAN DNS names: 3 fixed wildcard/service entries plus one per pod ordinal.
	dnsNames := make([]string, 0, 3+replicas)
	dnsNames = append(dnsNames,
		fmt.Sprintf("*.%s.%s.svc.cluster.local", headlessSvc, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", headlessSvc, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", ClientServiceName(cluster), namespace),
	)
	for i := range replicas {
		dnsNames = append(dnsNames,
			fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local", stsName, i, headlessSvc, namespace))
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cluster.Name + " CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating CA cert: %w", err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating server key: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cluster.Name},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("parsing CA cert: %w", err)
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating server cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling server key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER})

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TLSSecretName(cluster),
			Namespace: namespace,
			Labels:    CommonLabels(cluster),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
			"ca.crt":  caPEM,
		},
	}, nil
}
