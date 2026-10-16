package autoscale

import (
	"context"
	"testing"
	"time"

	"github.com/Publikey/runqy/control"
)

func TestScaleDownDrainsBeforeTeardown(t *testing.T) {
	a := newTestAutoscaler(t)
	prov := &fakeProvider{name: "mock"}
	ctx := context.Background()
	inst := addInstance(t, a, "i-1", StatusRunning, "w-1", time.Hour)
	hb := &workerHeartbeat{workerID: "w-1", status: "running", control: control.WorkerFields{ControlActions: control.Actions}}
	by := map[string]*workerHeartbeat{"i-1": hb}

	// 1st poll: the worker is asked to drain, the machine stays up.
	a.scaleDown(ctx, prov, "inference", []*Instance{inst}, []*Instance{inst}, by)
	if statusOf(t, a, "i-1") != StatusRunning || a.drainSent["i-1"] != "w-1" {
		t.Fatalf("after 1st poll: status %s, drainSent %v", statusOf(t, a, "i-1"), a.drainSent)
	}

	// Still draining (a task in flight): wait.
	hb.status = workerStatusDraining
	a.scaleDown(ctx, prov, "inference", []*Instance{inst}, []*Instance{inst}, by)
	if statusOf(t, a, "i-1") != StatusRunning {
		t.Fatal("destroyed while draining")
	}

	// Drained: torn down.
	hb.status = workerStatusDrained
	a.scaleDown(ctx, prov, "inference", []*Instance{inst}, []*Instance{inst}, by)
	if s := statusOf(t, a, "i-1"); s == StatusRunning {
		t.Fatalf("drained instance not torn down (status %s)", s)
	}
	if _, ok := a.drainSent["i-1"]; ok {
		t.Error("drain bookkeeping not cleared after teardown")
	}
}

func TestScaleDownLegacyWorkerDestroyedRightAway(t *testing.T) {
	a := newTestAutoscaler(t)
	inst := addInstance(t, a, "i-1", StatusRunning, "w-1", time.Hour)
	by := map[string]*workerHeartbeat{"i-1": {workerID: "w-1", status: "running"}} // no control_actions
	a.scaleDown(context.Background(), &fakeProvider{name: "mock"}, "inference", []*Instance{inst}, []*Instance{inst}, by)
	if statusOf(t, a, "i-1") == StatusRunning {
		t.Fatal("worker without control channel should be torn down as before")
	}
}

func TestScaleDownCancelledResumesWorker(t *testing.T) {
	a := newTestAutoscaler(t)
	inst := addInstance(t, a, "i-1", StatusRunning, "w-1", time.Hour)
	by := map[string]*workerHeartbeat{"i-1": {workerID: "w-1", status: "running", control: control.WorkerFields{ControlActions: control.Actions}}}
	prov := &fakeProvider{name: "mock"}
	a.scaleDown(context.Background(), prov, "inference", []*Instance{inst}, []*Instance{inst}, by)

	// Next poll the plan keeps it (it picked up a task): the drain is undone.
	a.scaleDown(context.Background(), prov, "inference", []*Instance{inst}, nil, by)
	if _, ok := a.drainSent["i-1"]; ok || statusOf(t, a, "i-1") != StatusRunning {
		t.Fatalf("drain not cancelled: %v / %s", a.drainSent, statusOf(t, a, "i-1"))
	}
}
