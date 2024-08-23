//go:build deadlock_test

package lock

import "github.com/sasha-s/go-deadlock"

// this type is used only in test for deadlock detection
type RWMutex struct {
	deadlock.RWMutex
}
