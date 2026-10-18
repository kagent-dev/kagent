// Command credentialprovider serves the e2e suite's credential provider to
// Substrate's egress gateway over mutual TLS, as the gateway requires of
// every provider it dials: the pod's servicedns.podcert.ate.dev certificate
// for its Service, and only a caller whose podidentity.podcert.ate.dev
// certificate names the gateway.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
	"github.com/kagent-dev/kagent/go/core/test/credentialprovider"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	listen := flag.String("listen-address", ":50051", "address the provider serves gRPC on")
	serverBundle := flag.String("server-cred-bundle", "/run/servicedns.podcert.ate.dev/credential-bundle.pem", "PEM private key and certificate chain the provider serves with")
	clientCA := flag.String("client-ca-file", "/run/podidentity.podcert.ate.dev/trust-bundle.pem", "PEM trust bundle a caller's certificate chains to")
	injector := flag.String("injector-identity", "", "SPIFFE ID of the egress gateway, the only caller answered")
	authority := flag.String("authority", "", "authority of the credential URIs the provider answers")
	refused := flag.String("refused", "", "comma-separated provider instances the provider refuses")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger, *listen, *serverBundle, *clientCA, *injector, *authority, *refused); err != nil {
		logger.Error("credential provider", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, listen, serverBundle, clientCA, injector, authority, refused string) error {
	if injector == "" || authority == "" {
		return errors.New("--injector-identity and --authority are required")
	}
	certificate, err := loadBundle(serverBundle)
	if err != nil {
		return err
	}
	authorities, err := os.ReadFile(clientCA)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(authorities) {
		return fmt.Errorf("%s holds no certificate", clientCA)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		VerifyConnection: func(state tls.ConnectionState) error {
			for _, id := range state.PeerCertificates[0].URIs {
				if id.String() == injector {
					return nil
				}
			}
			return fmt.Errorf("caller %v is not the egress gateway %s", state.PeerCertificates[0].URIs, injector)
		},
	})))
	provider := &credentialprovider.Provider{Authority: authority}
	if refused != "" {
		provider.Refused = strings.Split(refused, ",")
	}
	credproviderpb.RegisterCredentialProviderServer(server, logged{provider, logger})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	logger.Info("serving", "address", listen, "authority", authority, "refused", provider.Refused, "injector", injector)
	return server.Serve(listener)
}

// logged logs each answer, its URI, Actor and outcome, never the credential.
type logged struct {
	*credentialprovider.Provider
	logger *slog.Logger
}

func (l logged) FetchSecret(ctx context.Context, request *credproviderpb.FetchSecretRequest) (*credproviderpb.FetchSecretResponse, error) {
	response, err := l.Provider.FetchSecret(ctx, request)
	l.logger.InfoContext(ctx, "fetch", "uri", request.GetUri(), "actor", request.GetActorSpiffeId(), "error", err)
	return response, err
}

// loadBundle reads a pod certificate credential bundle: the private key and
// the certificate chain, leaf first.
func loadBundle(path string) (tls.Certificate, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return tls.Certificate{}, err
	}
	var certificate tls.Certificate
	for {
		var block *pem.Block
		block, contents = pem.Decode(contents)
		if block == nil {
			break
		}
		switch block.Type {
		case "CERTIFICATE":
			certificate.Certificate = append(certificate.Certificate, block.Bytes)
		case "PRIVATE KEY":
			if certificate.PrivateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
				return tls.Certificate{}, err
			}
		case "EC PRIVATE KEY":
			if certificate.PrivateKey, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
				return tls.Certificate{}, err
			}
		}
	}
	if certificate.PrivateKey == nil || len(certificate.Certificate) == 0 {
		return tls.Certificate{}, fmt.Errorf("%s holds no private key and certificate", path)
	}
	return certificate, nil
}
