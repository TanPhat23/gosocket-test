package ws_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"agent-task-manager/internal/ws"
)

const hubDeliveryTimeout = time.Second

// blockingClient is a Client whose Send does not return until release is closed.
type blockingClient struct {
	id      string
	taskID  string
	release chan struct{}
}

func (b *blockingClient) ID() string     { return b.id }
func (b *blockingClient) TaskID() string { return b.taskID }
func (b *blockingClient) Send([]byte) error {
	<-b.release
	return nil
}
func (b *blockingClient) Close() error { return nil }

// awaitRegistrations blocks until every Register call made before it has been processed.
// The hub drains its register channel in FIFO order, so a sentinel room becoming
// visible implies all earlier registrations are visible too.
func awaitRegistrations(t *testing.T, hub *ws.Hub) {
	t.Helper()
	sentinelRoom := fmt.Sprintf("sync-%d", time.Now().UnixNano())
	hub.Register(ws.NewMockClient("sentinel", sentinelRoom))
	waitForRoom(t, hub, sentinelRoom)
}

func waitForRoom(t *testing.T, hub *ws.Hub, room string) {
	t.Helper()
	deadline := time.Now().Add(hubDeliveryTimeout)
	for !hub.HasSubscribers(room) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for subscriber in room %q", room)
		}
		time.Sleep(time.Millisecond)
	}
}

func expectMessage(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(hubDeliveryTimeout):
		t.Fatalf("timed out waiting for a message")
	}
	return nil
}

func expectNoMessage(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("unexpected message delivered: %s", string(msg))
	case <-time.After(50 * time.Millisecond):
	}
}

// expectSentinelOnly asserts the channel's first delivered message is the sentinel,
// i.e. nothing else was delivered to this client before it.
func expectSentinelOnly(t *testing.T, ch <-chan []byte, sentinel string) {
	t.Helper()
	if got := string(expectMessage(t, ch)); got != sentinel {
		t.Fatalf("expected only %q, got leaked message %q", sentinel, got)
	}
}

func TestHubBroadcastGlobalReachesGlobalSubscribersOnly(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	globalA := ws.NewMockClient("global-a", "global")
	globalB := ws.NewMockClient("global-b", "global")
	taskScoped := ws.NewMockClient("task-client", "task-1")
	hub.Register(globalA)
	hub.Register(globalB)
	hub.Register(taskScoped)
	awaitRegistrations(t, hub)

	payload := []byte("global-event")
	hub.BroadcastGlobal(payload)

	for name, client := range map[string]*ws.MockClient{"global-a": globalA, "global-b": globalB} {
		if got := string(expectMessage(t, client.Messages())); got != string(payload) {
			t.Errorf("%s: expected %q, got %q", name, payload, got)
		}
	}

	hub.BroadcastToTask("task-1", []byte("sentinel"))
	expectSentinelOnly(t, taskScoped.Messages(), "sentinel")
}

func TestHubBroadcastGlobalWithoutSubscribersIsNoop(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	taskScoped := ws.NewMockClient("task-client", "task-1")
	hub.Register(taskScoped)
	awaitRegistrations(t, hub)

	hub.BroadcastGlobal([]byte("nobody-listening"))
	expectNoMessage(t, taskScoped.Messages())
}

func TestHubBroadcastTaskDiffReachesOnlyThatTaskSubscribers(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	taskA1 := ws.NewMockClient("a1", "task-a")
	taskA2 := ws.NewMockClient("a2", "task-a")
	taskB := ws.NewMockClient("b1", "task-b")
	globalC := ws.NewMockClient("g1", "global")
	hub.Register(taskA1)
	hub.Register(taskA2)
	hub.Register(taskB)
	hub.Register(globalC)
	awaitRegistrations(t, hub)

	if err := hub.BroadcastTaskDiff("task-a", "+added line"); err != nil {
		t.Fatalf("BroadcastTaskDiff returned error: %v", err)
	}

	for name, client := range map[string]*ws.MockClient{"a1": taskA1, "a2": taskA2} {
		var envelope ws.Message[string]
		if err := json.Unmarshal(expectMessage(t, client.Messages()), &envelope); err != nil {
			t.Fatalf("%s: failed to unmarshal diff envelope: %v", name, err)
		}
		if envelope.Type != ws.MessageTypeDiff || envelope.TaskID != "task-a" || envelope.Payload != "+added line" {
			t.Errorf("%s: unexpected diff envelope: %+v", name, envelope)
		}
	}

	hub.BroadcastToTask("task-b", []byte("sentinel-b"))
	hub.BroadcastToTask("global", []byte("sentinel-g"))
	expectSentinelOnly(t, taskB.Messages(), "sentinel-b")
	expectSentinelOnly(t, globalC.Messages(), "sentinel-g")
}

func TestHubBroadcastTaskDiffWithoutSubscribersIsNoop(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	if err := hub.BroadcastTaskDiff("unknown-task", "diff"); err != nil {
		t.Fatalf("expected nil error with no subscribers, got %v", err)
	}
	if hub.HasSubscribers("unknown-task") {
		t.Fatalf("broadcast must not create a room")
	}
}

func TestHubSlowClientDoesNotBlockOtherSubscribers(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	slow := &blockingClient{id: "slow", taskID: "task-x", release: release}
	fast := ws.NewMockClient("fast", "task-x")
	hub.Register(slow)
	hub.Register(fast)
	awaitRegistrations(t, hub)

	returned := make(chan struct{})
	go func() {
		hub.BroadcastToTask("task-x", []byte("chunk"))
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(hubDeliveryTimeout):
		t.Fatalf("BroadcastToTask blocked on a slow client")
	}

	if got := string(expectMessage(t, fast.Messages())); got != "chunk" {
		t.Fatalf("fast client expected chunk, got %q", got)
	}
}

func TestHubStopEndsRunLoopAndIsIdempotent(t *testing.T) {
	hub := ws.NewHub()
	runDone := make(chan struct{})
	go func() {
		hub.Run()
		close(runDone)
	}()

	hub.Stop()
	hub.Stop()

	select {
	case <-runDone:
	case <-time.After(hubDeliveryTimeout):
		t.Fatalf("Run did not return after Stop")
	}

	// Broadcasts after Stop must not panic or block.
	hub.BroadcastToTask("task-z", []byte("late"))
	hub.BroadcastGlobal([]byte("late-global"))
	if err := hub.BroadcastTaskDiff("task-z", "late-diff"); err != nil {
		t.Fatalf("BroadcastTaskDiff after Stop returned error: %v", err)
	}
}

func TestHubUnregisterStopsDelivery(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("gone", "task-u")
	hub.Register(client)
	waitForRoom(t, hub, "task-u")

	hub.Unregister(client)
	deadline := time.Now().Add(hubDeliveryTimeout)
	for hub.HasSubscribers("task-u") {
		if time.Now().After(deadline) {
			t.Fatalf("client still subscribed after Unregister")
		}
		time.Sleep(time.Millisecond)
	}

	hub.BroadcastToTask("task-u", []byte("after-unregister"))
	expectNoMessage(t, client.Messages())
}

func TestMockClientCloseIsIdempotentAndRejectsSend(t *testing.T) {
	client := ws.NewMockClient("closer", "task-c")
	if err := client.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close must be a no-op, got %v", err)
	}
	if err := client.Send([]byte("late")); err == nil {
		t.Fatalf("Send after Close must return an error")
	}
	select {
	case _, ok := <-client.Messages():
		if ok {
			t.Fatalf("Messages channel must be closed after Close")
		}
	case <-time.After(hubDeliveryTimeout):
		t.Fatalf("Messages channel was not closed by Close")
	}
}
