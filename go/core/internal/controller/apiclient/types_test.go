package apiclient

import (
	"sync"
	"testing"
)

func TestRegisterTypesConcurrent(t *testing.T) {
	var group sync.WaitGroup
	for range 10 {
		group.Go(RegisterTypes)
	}
	group.Wait()
}
