//go:build !deadlock_test

package lock

import "sync"

type RWMutex struct {
	sync.RWMutex
}
