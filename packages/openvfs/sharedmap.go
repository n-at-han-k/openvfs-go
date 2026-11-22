package openvfs

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Transpiled from src/openvfsfuse/sharedmap.{h,cpp}.

// HydJobState is the state a hydration job can be in. A job starts out Running
// and is moved to one of the other states by the socket thread.
type HydJobState int

const (
	HydSuccess HydJobState = 0
	HydRunning HydJobState = 1
	HydFailed  HydJobState = -1
)

func (s HydJobState) String() string {
	switch s {
	case HydSuccess:
		return "success"
	case HydFailed:
		return "failed"
	case HydRunning:
		return "running"
	}
	return "undef"
}

// HydJob represents a hydration job that is stored in the job queue and passes
// data between fuse- and worker thread.
type HydJob struct {
	State HydJobState
}

// HydJobResult is the outcome of a hydration job after it left the Running
// state.
type HydJobResult int

const (
	HydResultSucceeded HydJobResult = iota // the client reported the file as hydrated
	HydResultFailed                        // the client reported an error, or the request could not be sent
	HydResultTimedOut                      // the client did not answer within the timeout
	HydResultLost                          // the job vanished from the map, which should not happen
)

type SharedMap struct {
	mu   sync.Mutex
	data map[int]HydJob
	// waiters stands in for _cv: closing a channel wakes its waiter, which
	// then re-evaluates the predicate the way condition_variable::wait() does.
	waiters []chan struct{}
	pid     int64
}

func NewSharedMap() *SharedMap {
	return &SharedMap{data: make(map[int]HydJob)}
}

// notifyAllLocked is _cv.notify_all().
func (m *SharedMap) notifyAllLocked() {
	for _, ch := range m.waiters {
		close(ch)
	}
	m.waiters = nil
}

func (m *SharedMap) Insert(key int, value HydJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	m.notifyAllLocked()
}

func (m *SharedMap) Get(key int) (HydJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.data[key]
	return job, ok
}

func (m *SharedMap) Set(key int, value HydJob) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.data[key]; !found {
		return false
	}
	m.data[key] = value
	m.notifyAllLocked()
	return true
}

func (m *SharedMap) Remove(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.data[id]; !found {
		return false
	}
	delete(m.data, id)
	m.notifyAllLocked()
	return true
}

// WaitForJob blocks until the job identified by key leaves the Running state,
// or until timeout has elapsed.
//
// The wait is bounded by wall-clock time rather than by a number of polls, and
// a state change is observed as soon as the socket thread publishes it.
func (m *SharedMap) WaitForJob(key int, timeout time.Duration) HydJobResult {
	deadline := time.Now().Add(timeout)

	m.mu.Lock()
	defer m.mu.Unlock()

	for {
		// The predicate handed to condition_variable::wait_for(): the job is
		// gone, or it is no longer running. A wake-up caused by any other key
		// finds the predicate false and goes back to waiting.
		job, found := m.data[key]
		if !found {
			return HydResultLost
		}
		if job.State != HydRunning {
			if job.State == HydSuccess {
				return HydResultSucceeded
			}
			return HydResultFailed
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return HydResultTimedOut
		}

		ch := make(chan struct{})
		m.waiters = append(m.waiters, ch)
		m.mu.Unlock()

		timer := time.NewTimer(remaining)
		select {
		case <-ch:
		case <-timer.C:
		}
		timer.Stop()

		m.mu.Lock()
		m.dropWaiterLocked(ch)
	}
}

// dropWaiterLocked forgets a channel that was woken by the deadline rather than
// by a notification, so a run of timeouts cannot grow the waiter list.
func (m *SharedMap) dropWaiterLocked(ch chan struct{}) {
	for i, c := range m.waiters {
		if c == ch {
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			return
		}
	}
}

func (m *SharedMap) SetDesktopClientPID(pid int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pid = pid
}

func (m *SharedMap) DesktopClientPID() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pid
}

func (m *SharedMap) PrintAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]int, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	// std::map iterates in key order.
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("%d: %s\n", k, m.data[k].State)
	}
}
