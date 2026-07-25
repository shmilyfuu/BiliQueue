package main

import (
	"testing"
	"time"
)

func TestQueueCommandsShareStateMutation(t *testing.T) {
	app := newApp(t.TempDir())
	ok, detail := app.addManualUser("Native UI")
	if !ok {
		t.Fatalf("manual add failed: %s", detail)
	}
	state := app.state()
	if len(state.Queue) != 1 || !state.Queue[0].Manual {
		t.Fatalf("unexpected queue: %#v", state.Queue)
	}
	app.setQueuePaused(true)
	if !app.state().Paused {
		t.Fatal("pause command did not update state")
	}
	if !app.removeQueueUser(state.Queue[0].ID) {
		t.Fatal("remove command did not find user")
	}
	if len(app.state().Queue) != 0 {
		t.Fatal("queue was not emptied")
	}
}

func TestStateObserverReceivesLatestQueueState(t *testing.T) {
	app := newApp(t.TempDir())
	states, cancel := app.subscribeState()
	defer cancel()
	select {
	case <-states:
	case <-time.After(time.Second):
		t.Fatal("initial state was not delivered")
	}
	if ok, detail := app.addManualUser("Observer"); !ok {
		t.Fatalf("manual add failed: %s", detail)
	}
	select {
	case state := <-states:
		if len(state.Queue) != 1 || state.Queue[0].Username != "Observer" {
			t.Fatalf("unexpected observed state: %#v", state.Queue)
		}
	case <-time.After(time.Second):
		t.Fatal("updated state was not delivered")
	}
}
