package upgrade

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	migrations "github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

const (
	postgresServiceName = "kagent-postgresql"
	// The login user and role kagent install creates for the controller.
	postgresLoginUser = "kagent_user"
	postgresRole      = "kagent_owner"
)

// pgTrackVersion returns the current applied migration version.
func pgTrackVersion(t *testing.T, env upgradeEnv, table string) int {
	t.Helper()

	raw := pgQuery(t, env, fmt.Sprintf(
		"SELECT CASE WHEN to_regclass('%[1]s.%[2]s') IS NULL THEN 0 ELSE (SELECT COALESCE(MAX(version_id), 0) FROM %[1]s.%[2]s WHERE is_applied) END",
		kagentSchema, table))
	return parseInt(t, raw, table+" version")
}

// pgSchemaDump returns the normalized schema-only dump of one schema, suitable
// for structural equality comparison between a migrated and a clean install.
func pgSchemaDump(t *testing.T, env upgradeEnv, schema string) string {
	t.Helper()

	pod := podNameForSelector(t, env, postgresSelector)
	out := kubectl(t, env, 2*time.Minute,
		"exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"pg_dump", "--schema-only", "--no-owner", "--no-privileges",
		"-U", postgresSuperuser, "-d", postgresDatabase, "--schema", schema,
	)
	return normalizeSchemaDump(out, schema)
}

// normalizeSchemaDump strips the non-structural noise from a pg_dump so two
// dumps of the same logical schema compare equal: comments, blank lines, the
// session-setup preamble (SET / SELECT pg_catalog.set_config), and psql
// meta-commands. The latter matters because pg_dump on PostgreSQL 17+ wraps the
// output in `\restrict`/`\unrestrict` lines carrying a per-invocation random
// token, so two dumps of an identical schema would otherwise never compare equal.
// References to schema are replaced with a placeholder so dumps of differently
// named schemas compare equal.
func normalizeSchemaDump(dump, schema string) string {
	lines := strings.Split(dump, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.HasPrefix(trimmed, "SET ") {
			continue
		}
		if strings.HasPrefix(trimmed, "SELECT pg_catalog.set_config") {
			continue
		}
		// psql meta-commands (\restrict, \unrestrict, \connect): non-structural,
		// and the restrict tokens are randomized per dump.
		if strings.HasPrefix(trimmed, `\`) {
			continue
		}
		trimmed = strings.ReplaceAll(trimmed, schema+".", "<schema>.")
		if trimmed == "CREATE SCHEMA "+schema+";" {
			trimmed = "CREATE SCHEMA <schema>;"
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

// buildCleanInstallSchema provisions a throwaway schema, applies the embedded
// migrations up to the latest version via the real RunUp code path, and
// returns its normalized schema. This is the "clean HEAD install" reference the
// design's round-trip gate compares an upgraded database against. The bundled
// database only admits application logins to the kagent database, so the
// reference lives in a sibling schema rather than a separate database.
func buildCleanInstallSchema(t *testing.T, env upgradeEnv, schema string, vectorEnabled bool) string {
	t.Helper()

	pgExec(t, env, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
	pgExec(t, env, fmt.Sprintf("CREATE SCHEMA %s AUTHORIZATION %s", schema, postgresRole))
	// Best-effort drop with its own context: t.Context() is already canceled by
	// the time cleanups run, so a normal kubectl call here would always error.
	t.Cleanup(func() { dropSchemaBestEffort(env, schema) })

	applyEmbeddedMigrations(t, env, schema, vectorEnabled)

	return pgSchemaDump(t, env, schema)
}

// applyEmbeddedMigrations migrates schema the way the controller does: as the
// kagent login user, after assuming the kagent role.
func applyEmbeddedMigrations(t *testing.T, env upgradeEnv, schema string, vectorEnabled bool) {
	t.Helper()

	localPort, stop := startPortForward(t, env, postgresServiceName, 5432)
	defer stop()

	sources := migrations.BuiltinSourcesInSchema(vectorEnabled, schema, consts.DefaultPgvectorSchema)
	require.NoError(t, migrations.RunUpAsRole(t.Context(), postgresURL(t, env, localPort), postgresRole, sources),
		"apply embedded migrations to schema %s", schema)
}

func migrateEmbeddedSourcesTo(t *testing.T, env upgradeEnv, targets map[string]int, vectorEnabled bool) {
	t.Helper()

	localPort, stop := startPortForward(t, env, postgresServiceName, 5432)
	defer stop()
	url := postgresURL(t, env, localPort)

	for _, source := range slices.Backward(migrations.BuiltinSourcesInSchema(vectorEnabled, kagentSchema, consts.DefaultPgvectorSchema)) {
		target, ok := targets[source.Name]
		require.True(t, ok, "missing rollback target for migration source %s", source.Name)
		err := migrations.WithProviderAsRole(t.Context(), url, postgresRole, source, func(provider *goose.Provider) error {
			_, err := provider.DownTo(t.Context(), int64(target))
			return err
		})
		require.NoError(t, err, "roll back %s migrations to version %d", source.Name, target)
	}
}

func scaleController(t *testing.T, env upgradeEnv, replicas int) {
	t.Helper()

	kubectl(t, env, 2*time.Minute,
		"scale", "deployment/kagent-controller",
		"-n", env.namespace,
		fmt.Sprintf("--replicas=%d", replicas),
	)
	if replicas == 0 {
		require.Eventually(t, func() bool {
			pods, err := podNamesForSelectorE(t, env, controllerSelector)
			return err == nil && len(pods) == 0
		}, 2*time.Minute, 2*time.Second, "controller pods did not terminate after scale to zero")
		return
	}
	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
}

// caPoolEntry is one CA in a Substrate CA pool Secret's "pool" JSON.
type caPoolEntry struct {
	ID                 string
	SigningKeyPKCS8    []byte
	RootCertificateDER []byte
}

// postgresURL returns a connection URL for a port-forward to the bundled
// database. The database rejects network logins without a client certificate, so
// this signs one for the kagent login user with the active postgres-ca-pool CA,
// the same CA that issues the controller's Pod Certificate.
func postgresURL(t *testing.T, env upgradeEnv, localPort int) string {
	t.Helper()

	raw := kubectl(t, env, time.Minute, "get", "secret", "postgres-ca-pool",
		"-n", "podcertificate-controller-system", "-o", "jsonpath={.data.pool}")
	poolJSON, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	require.NoError(t, err, "decode postgres-ca-pool")
	var pool struct {
		CAs              []caPoolEntry
		ActiveForSigning string
	}
	require.NoError(t, json.Unmarshal(poolJSON, &pool), "parse postgres-ca-pool")
	active := slices.IndexFunc(pool.CAs, func(ca caPoolEntry) bool { return ca.ID == pool.ActiveForSigning })
	require.NotEqual(t, -1, active, "postgres-ca-pool has no active CA %q", pool.ActiveForSigning)
	caCert, err := x509.ParseCertificate(pool.CAs[active].RootCertificateDER)
	require.NoError(t, err, "parse postgres CA certificate")
	caKey, err := x509.ParsePKCS8PrivateKey(pool.CAs[active].SigningKeyPKCS8)
	require.NoError(t, err, "parse postgres CA key")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: postgresLoginUser},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, caCert, &key.PublicKey, caKey)
	require.NoError(t, err, "sign postgres client certificate")
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))

	// The server certificate names the in-cluster Service, not 127.0.0.1, so
	// the forward only encrypts; it does not verify the server.
	return fmt.Sprintf("postgres://%s@127.0.0.1:%d/%s?sslmode=require&sslcert=%s&sslkey=%s",
		postgresLoginUser, localPort, postgresDatabase, url.QueryEscape(certPath), url.QueryEscape(keyPath))
}

// dropSchemaBestEffort removes a scratch schema, ignoring all errors. It
// uses its own background context so it still runs during test teardown, after
// t.Context() has been canceled, and never fails the test.
func dropSchemaBestEffort(env upgradeEnv, schema string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	podOut, err := exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext, "get", "pods",
		"-n", env.namespace, "-l", postgresSelector,
		"-o", "jsonpath={.items[0].metadata.name}",
	).Output()
	if err != nil {
		return
	}
	pod := strings.TrimSpace(string(podOut))
	if pod == "" {
		return
	}
	_ = exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext, "exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"psql", "-U", postgresSuperuser, "-d", postgresDatabase, "-tAc",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
	).Run()
}

var forwardingPortRE = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

// startPortForward opens a kubectl port-forward to a service and returns the
// chosen local port and a stop function.
func startPortForward(t *testing.T, env upgradeEnv, service string, remotePort int) (int, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext,
		"port-forward",
		"-n", env.namespace,
		"svc/"+service,
		fmt.Sprintf(":%d", remotePort),
	)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "pipe port-forward stdout")
	require.NoError(t, cmd.Start(), "start port-forward")

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}

	portCh := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if match := forwardingPortRE.FindStringSubmatch(scanner.Text()); match != nil {
				p, convErr := strconv.Atoi(match[1])
				if convErr == nil {
					select {
					case portCh <- p:
					default:
					}
				}
			}
		}
	}()

	select {
	case port := <-portCh:
		return port, stop
	case <-time.After(15 * time.Second):
		stop()
		t.Fatalf("port-forward to svc/%s did not become ready", service)
		return 0, func() {}
	}
}
