package backup

import "sync"

type repoLockTable struct {
	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

func newRepoLockTable() *repoLockTable {
	return &repoLockTable{locks: make(map[string]*sync.RWMutex)}
}

func (t *repoLockTable) get(stackName string) *sync.RWMutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	lock, ok := t.locks[stackName]
	if !ok {
		lock = &sync.RWMutex{}
		t.locks[stackName] = lock
	}
	return lock
}
