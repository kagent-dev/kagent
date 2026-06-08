package substrate

import "testing"

func TestDeleteActorEmptyID(t *testing.T) {
	t.Parallel()
	done, err := deleteActor(t.Context(), nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Fatal("expected done for empty actor id")
	}
}
