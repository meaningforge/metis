package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/meaningforge/metis/execution/datasource"
)

func TestAdmissionGateGrantsQueuedRequestsInFIFOOrder(t *testing.T) {
	gate := testAdmissionGate(t, 1, 3, time.Second)
	if err := gate.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	granted := make(chan int, 2)
	for id := 1; id <= 2; id++ {
		go func() {
			if err := gate.acquire(context.Background()); err != nil {
				granted <- -id
				return
			}
			granted <- id
		}()
		awaitAdmissionQueue(t, gate, id)
	}

	gate.release()
	if got := <-granted; got != 1 {
		t.Fatalf("first queued grant = %d, want 1", got)
	}
	select {
	case got := <-granted:
		t.Fatalf("second waiter bypassed active first waiter: %d", got)
	default:
	}
	gate.release()
	if got := <-granted; got != 2 {
		t.Fatalf("second queued grant = %d, want 2", got)
	}
	gate.release()
}

func TestAdmissionGateCancellationAndTimeoutDoNotLeakPermits(t *testing.T) {
	gate := testAdmissionGate(t, 1, 2, 10*time.Millisecond)
	if err := gate.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- gate.acquire(cancelled) }()
	awaitAdmissionQueue(t, gate, 1)
	cancel()
	assertAdmissionCode(t, <-cancelDone, ExecutionCancelled)

	timedOut := make(chan error, 1)
	go func() { timedOut <- gate.acquire(context.Background()) }()
	awaitAdmissionQueue(t, gate, 1)
	assertAdmissionCode(t, <-timedOut, ExecutionCapacity)

	gate.release()
	if err := gate.acquire(context.Background()); err != nil {
		t.Fatalf("permit leaked after queue faults: %v", err)
	}
	gate.release()
}

func testAdmissionGate(t *testing.T, concurrency, queue int, timeout time.Duration) *admissionGate {
	t.Helper()
	maxRows, maxBytes := int64(1), int64(1)
	gate, err := newAdmissionGate(datasource.DataSourcePolicy{
		QueryTimeout: "1s", MaxRows: &maxRows, MaxBytes: &maxBytes,
		MaxConcurrency: &concurrency, MaxQueue: &queue, QueueTimeout: timeout.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return gate
}

func awaitAdmissionQueue(t *testing.T, gate *admissionGate, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		gate.mu.Lock()
		got := len(gate.waiters)
		gate.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("admission queue did not reach %d waiters", want)
}

func assertAdmissionCode(t *testing.T, err error, want ExecutionErrorCode) {
	t.Helper()
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != want {
		t.Fatalf("error = %#v, want %s", err, want)
	}
}
