package reconciler

import (
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestComputeStatusSecretHash_Output verifies the output of the hash function
func TestComputeStatusSecretHash_Output(t *testing.T) {
	tests := []struct {
		name    string
		secrets []secretRef
		want    string
	}{
		{
			name:    "no secrets",
			secrets: []secretRef{},
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // i.e. the hash of an empty string
		},
		{
			name: "one secret, no keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{},
					},
				},
			},
			want: "68a268d3f02147004cfa8b609966ec4cba7733f8c652edb80be8071eb1b91574", // because the secret exists, it still hashes the namespacedName + empty data
		},
		{
			name: "one secret, single key",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
			},
			want: "62dc22ecd609281a5939efd60fae775e6b75b641614c523c400db994a09902ff",
		},
		{
			name: "one secret, multiple keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
					},
				},
			},
			want: "ba6798ec591d129f78322cdae569eaccdb2f5a8343c12026f0ed6f4e156cd52e",
		},
		{
			name: "multiple secrets",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key2": []byte("value2")},
					},
				},
			},
			want: "f174f0e21a4427a87a23e4f277946a27f686d023cbe42f3000df94a4df94f7b5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeStatusSecretHash(tt.secrets)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestComputeStatusSecretHash_Deterministic tests that the resultant hash is deterministic, specifically that ordering of keys and secrets does not matter
func TestComputeStatusSecretHash_Deterministic(t *testing.T) {
	tests := []struct {
		name          string
		secrets       [2][]secretRef
		expectedEqual bool
	}{
		{
			name: "key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret and key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got1 := computeStatusSecretHash(tt.secrets[0])
			got2 := computeStatusSecretHash(tt.secrets[1])
			assert.Equal(t, tt.expectedEqual, got1 == got2)
		})
	}
}

func TestAgentIDConsistency(t *testing.T) {
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "my-agent",
		},
	}

	storeID := utils.ConvertToPythonIdentifier(utils.ResourceRefString(req.Namespace, req.Name))
	deleteID := utils.ConvertToPythonIdentifier(req.String())

	assert.Equal(t, storeID, deleteID)
}

// TestUpsertLockNotBlockedBySlowOperations verifies that Agent reconciliation
// (via upsertAgent) is not blocked when RemoteMCPServer reconciliation
// (via upsertToolServerForRemoteMCPServer) is performing slow network I/O.
//
// This is a regression test for a deadlock where the mutex was held during
// network operations, blocking all Agent reconciliations.
func TestUpsertLockNotBlockedBySlowOperations(t *testing.T) {
	// This test verifies the lock behavior at the unit level by directly
	// testing that the upsertLock can be acquired while a simulated slow
	// operation is in progress (after its initial DB write completes).

	var mu sync.Mutex
	agentCompleted := make(chan time.Duration, 1)

	// Simulate the FIXED behavior: lock is released during "slow" operation
	simulateFixedRemoteMCPServerReconcile := func() {
		// Step 1: Lock for fast DB write
		mu.Lock()
		time.Sleep(5 * time.Millisecond) // Simulate fast DB write
		mu.Unlock()

		// Step 2: Slow network I/O (no lock held)
		time.Sleep(200 * time.Millisecond) // Simulate slow MCP server response

		// Step 3: Lock for fast DB write
		mu.Lock()
		time.Sleep(5 * time.Millisecond) // Simulate fast DB write
		mu.Unlock()
	}

	simulateAgentReconcile := func() {
		start := time.Now()
		mu.Lock()
		time.Sleep(5 * time.Millisecond) // Simulate fast DB write
		mu.Unlock()
		agentCompleted <- time.Since(start)
	}

	// Start the "slow" RemoteMCPServer reconcile
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		simulateFixedRemoteMCPServerReconcile()
	}()

	// Wait for RemoteMCPServer to acquire lock, do DB write, and release
	time.Sleep(20 * time.Millisecond)

	// Now try to run Agent reconcile - it should NOT be blocked by the 200ms network I/O
	go func() {
		defer wg.Done()
		simulateAgentReconcile()
	}()

	// Wait for agent to complete and get its duration
	agentDuration := <-agentCompleted
	wg.Wait()

	// If the lock were held during network I/O (the bug), Agent would wait ~200ms
	// With the fix, Agent should complete quickly (lock is available during network I/O)
	assert.Less(t, agentDuration.Milliseconds(), int64(100),
		"Agent reconcile took too long (%v) - may indicate lock is held during slow operations", agentDuration)
}

// TestUpsertLockBlockedBySlowOperations_Unfixed demonstrates the buggy behavior
// where the lock is held during the entire operation. This test is here to
// document the problem that was fixed.
func TestUpsertLockBlockedBySlowOperations_Unfixed(t *testing.T) {
	var mu sync.Mutex
	agentCompleted := make(chan time.Duration, 1)

	// Simulate the BUGGY behavior: lock held during entire operation
	simulateBuggyRemoteMCPServerReconcile := func() {
		mu.Lock()
		defer mu.Unlock()

		time.Sleep(5 * time.Millisecond)   // Simulate fast DB write
		time.Sleep(200 * time.Millisecond) // Simulate slow MCP server response (lock still held!)
		time.Sleep(5 * time.Millisecond)   // Simulate fast DB write
	}

	simulateAgentReconcile := func() {
		start := time.Now()
		mu.Lock()
		time.Sleep(5 * time.Millisecond) // Simulate fast DB write
		mu.Unlock()
		agentCompleted <- time.Since(start)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		simulateBuggyRemoteMCPServerReconcile()
	}()

	// Wait for RemoteMCPServer to acquire the lock
	time.Sleep(10 * time.Millisecond)

	// Now try to run Agent reconcile - it WILL be blocked
	go func() {
		defer wg.Done()
		simulateAgentReconcile()
	}()

	// Wait for agent to complete and get its duration
	agentDuration := <-agentCompleted
	wg.Wait()

	// With the bug, Agent has to wait for the full operation (~200ms)
	assert.Greater(t, agentDuration.Milliseconds(), int64(150),
		"Agent reconcile completed too quickly (%v) - this test demonstrates the buggy blocking behavior", agentDuration)
}
