package database

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

func TestStoreRetriesFailedLogins(t *testing.T) {
	t.Run("Given PostgreSQL drops the first two logins, when a caller stores a memory, assert that the memory is stored once", func(t *testing.T) {
		inspect := NewClient(setupTestDB(t), "public")
		proxy := newLoginProxy(t, dropFirst(2))
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Minute))

		require.NoError(t, client.StoreAgentMemories(t.Context(), testMemory()))

		memories, err := inspect.ListAgentMemories(t.Context(), "team-a/agent", "owner")
		require.NoError(t, err)
		require.Len(t, memories, 1)
		require.Equal(t, int32(3), proxy.logins.Load())
	})

	t.Run("Given PostgreSQL drops the first two logins, when a caller stores memories in one transaction, assert that each memory is stored once", func(t *testing.T) {
		inspect := NewClient(setupTestDB(t), "public")
		proxy := newLoginProxy(t, dropFirst(2))
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Minute))

		require.NoError(t, client.StoreAgentMemories(t.Context(), testMemory(), testMemory()))

		memories, err := inspect.ListAgentMemories(t.Context(), "team-a/agent", "owner")
		require.NoError(t, err)
		require.Len(t, memories, 2)
		require.Equal(t, int32(3), proxy.logins.Load())
	})

	t.Run("Given PostgreSQL drops the first login, when a caller deletes memories, assert that the memories are deleted", func(t *testing.T) {
		inspect := NewClient(setupTestDB(t), "public")
		require.NoError(t, inspect.StoreAgentMemories(t.Context(), testMemory()))
		proxy := newLoginProxy(t, dropFirst(1))
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Minute))

		require.NoError(t, client.DeleteAgentMemory(t.Context(), "team-a/agent", "owner"))

		memories, err := inspect.ListAgentMemories(t.Context(), "team-a/agent", "owner")
		require.NoError(t, err)
		require.Empty(t, memories)
		require.Equal(t, int32(2), proxy.logins.Load())
	})

	t.Run("Given PostgreSQL holds the first login past its connect timeout, when a caller reads, assert that the read succeeds on the next login", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, holdFirst(1))
		config := dbtest.PoolConfig(sharedConnStr)
		config.ConnConfig.ConnectTimeout = 500 * time.Millisecond
		client := proxy.client(t, config, WithConnectRetry(time.Minute))

		_, err := client.ListAgentMemories(t.Context(), "team-a/agent", "owner")

		require.NoError(t, err)
		require.Equal(t, int32(2), proxy.logins.Load())
	})
}

func TestStoreConnectRetryLimits(t *testing.T) {
	t.Run("Given PostgreSQL drops every login, when the retry budget runs out, assert that the call fails with the connect error", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, dropAll)
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Second))

		start := time.Now()
		_, err := client.ListAgentMemories(t.Context(), "team-a/agent", "owner")
		elapsed := time.Since(start)

		require.True(t, IsUnavailable(err), "want a connect error, got %v", err)
		require.GreaterOrEqual(t, elapsed, time.Second)
		require.Less(t, elapsed, 2*time.Second)
		require.Greater(t, proxy.logins.Load(), int32(1))
	})

	t.Run("Given PostgreSQL drops every login, when the caller's deadline expires first, assert that the call returns at the deadline", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, dropAll)
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Minute))
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := client.ListAgentMemories(ctx, "team-a/agent", "owner")

		require.Less(t, time.Since(start), time.Second)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.True(t, IsUnavailable(err), "want the connect error kept, got %v", err)
	})

	t.Run("Given PostgreSQL never answers the TCP handshake, when the retry budget runs out, assert that the call fails within the budget plus one dial", func(t *testing.T) {
		config, err := poolConfig(&PostgresConfig{Pool: dbtest.PoolConfig("postgres://kagent@" + unansweredAddress(t) + "/kagent?sslmode=disable")})
		require.NoError(t, err)
		pool, err := pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		client := NewClient(pool, "public", WithConnectRetry(15*time.Second))
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		start := time.Now()
		_, err = client.ListAgentMemories(ctx, "team-a/agent", "owner")
		elapsed := time.Since(start)

		require.True(t, IsUnavailable(err), "want a connect error, got %v", err)
		require.GreaterOrEqual(t, elapsed, 15*time.Second)
		require.Less(t, elapsed, 25*time.Second, "a dial outlived its bound: %v", err)
	})

	t.Run("Given a wrong password, when a caller reads, assert that the login is tried once", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, forwardAll)
		config := dbtest.PoolConfig(sharedConnStr)
		config.ConnConfig.Password = "wrong"
		client := proxy.client(t, config, WithConnectRetry(time.Minute))

		start := time.Now()
		_, err := client.ListAgentMemories(t.Context(), "team-a/agent", "owner")

		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "28P01", pgErr.Code)
		require.Less(t, time.Since(start), defaultInitialDelay)
		require.Equal(t, int32(1), proxy.logins.Load())
	})

	scenarios := []struct {
		name string
		opts []ClientOption
	}{
		{"Given no connect retry option, when PostgreSQL drops the first login, assert that the call fails after one login", nil},
		{"Given a zero connect retry timeout, when PostgreSQL drops the first login, assert that the call fails after one login", []ClientOption{WithConnectRetry(0)}},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			setupTestDB(t)
			proxy := newLoginProxy(t, dropFirst(1))
			client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), scenario.opts...)

			_, err := client.ListAgentMemories(t.Context(), "team-a/agent", "owner")

			require.True(t, IsUnavailable(err), "want a connect error, got %v", err)
			require.Equal(t, int32(1), proxy.logins.Load())
		})
	}

	t.Run("Given a connected database, when a statement fails, assert that the call fails without waiting", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, forwardAll)
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(time.Minute))

		start := time.Now()
		err := client.DeleteSessionShare(t.Context(), "not-a-uuid", "owner")

		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Less(t, time.Since(start), defaultInitialDelay)
	})

	t.Run("Given a retry budget shorter than a query, when the query runs past the budget, assert that it returns its rows", func(t *testing.T) {
		setupTestDB(t)
		proxy := newLoginProxy(t, forwardAll)
		client := proxy.client(t, dbtest.PoolConfig(sharedConnStr), WithConnectRetry(100*time.Millisecond))

		got, err := queryOne(t.Context(), client.db, `SELECT 1 FROM pg_sleep(0.3)`, pgx.RowTo[int32])

		require.NoError(t, err)
		require.Equal(t, int32(1), got)
	})
}

func testMemory() *Memory {
	return &Memory{AgentName: "team-a/agent", UserID: "owner", Embedding: makeEmbedding(0.5)}
}

type loginAction int

const (
	forwardLogin loginAction = iota
	// dropLogin closes the connection after the startup message, like a proxy cutting a login.
	dropLogin
	// holdLogin never answers, like a resuming database.
	holdLogin
)

func dropFirst(n int32) func(int32) loginAction {
	return func(login int32) loginAction {
		if login <= n {
			return dropLogin
		}
		return forwardLogin
	}
}

func holdFirst(n int32) func(int32) loginAction {
	return func(login int32) loginAction {
		if login <= n {
			return holdLogin
		}
		return forwardLogin
	}
}

func dropAll(int32) loginAction { return dropLogin }

func forwardAll(int32) loginAction { return forwardLogin }

// unansweredAddress returns a loopback address with a full listen queue, so a
// dial hangs as it does against a host that is down.
func unansweredAddress(t *testing.T) string {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Close(fd) })
	require.NoError(t, syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}))
	require.NoError(t, syscall.Listen(fd, 1))
	bound, err := syscall.Getsockname(fd)
	require.NoError(t, err)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(bound.(*syscall.SockaddrInet4).Port))
	for range 16 {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err != nil {
			return address
		}
		t.Cleanup(func() { _ = conn.Close() })
	}
	t.Fatal("listen queue never filled")
	return ""
}

// loginProxy sits in front of the test database and handles each login by its
// 1-based position. Cancel requests are not counted.
type loginProxy struct {
	address string
	logins  atomic.Int32
}

func newLoginProxy(t *testing.T, plan func(login int32) loginAction) *loginProxy {
	t.Helper()
	upstream, err := pgconn.ParseConfig(sharedConnStr)
	require.NoError(t, err)
	upstreamAddress := net.JoinHostPort(upstream.Host, strconv.Itoa(int(upstream.Port)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = listener.Close()
	})
	proxy := &loginProxy{address: listener.Addr().String()}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go proxy.serve(conn, upstreamAddress, plan, done)
		}
	}()
	return proxy
}

func (p *loginProxy) serve(conn net.Conn, upstreamAddress string, plan func(int32) loginAction, done <-chan struct{}) {
	defer conn.Close()
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	const cancelRequestCode = 80877102
	if binary.BigEndian.Uint32(header[4:]) == cancelRequestCode {
		return
	}
	switch plan(p.logins.Add(1)) {
	case dropLogin:
		_, _ = io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint32(header[:4]))-int64(len(header)))
		return
	case holdLogin:
		<-done
		return
	}
	upstream, err := net.Dial("tcp", upstreamAddress)
	if err != nil {
		return
	}
	defer upstream.Close()
	if _, err := upstream.Write(header); err != nil {
		return
	}
	copied := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, conn); copied <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, upstream); copied <- struct{}{} }()
	select {
	case <-copied:
	case <-done:
	}
}

// client returns a Client that logs in through the proxy on first use.
func (p *loginProxy) client(t *testing.T, config *pgxpool.Config, opts ...ClientOption) *Client {
	t.Helper()
	config, err := poolConfig(&PostgresConfig{Pool: config, VectorEnabled: true, Schema: "public", VectorSchema: "public"})
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(p.address)
	require.NoError(t, err)
	portNumber, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	config.ConnConfig.Host, config.ConnConfig.Port = host, uint16(portNumber)
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return NewClient(pool, "public", opts...)
}
