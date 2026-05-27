package substrate

import "testing"

func TestAdvanceActorDeleteEmptyID(t *testing.T) {
	t.Parallel()
	c := &Client{}
	done, err := c.AdvanceActorDelete(t.Context(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Fatal("expected done for empty actor id")
	}
}
