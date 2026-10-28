package control

import "testing"

func TestRequestValidate(t *testing.T) {
	ok := []Request{
		{Action: ActionDrain},
		{Action: ActionResume, Target: "inference"},
		{Action: ActionCodeUpdate, Timeout: "10m"},
		{Action: ActionWorkerUpdate, Version: "0.4.0"},
		{Action: ActionWorkerUpdate, Version: "v0.4.0-rc.1"},
		{Action: ActionConfigReload},
	}
	for _, r := range ok {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := []Request{
		{Action: "reboot"},
		{Action: ActionWorkerUpdate},
		{Action: ActionWorkerUpdate, Version: "latest"},
		{Action: ActionDrain, Version: "0.4.0"},
		{Action: ActionDrain, Timeout: "soon"},
	}
	for _, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%+v accepted", r)
		}
	}
}

func TestParseWorkerFields(t *testing.T) {
	f := ParseWorkerFields(map[string]string{
		"version":         "0.4.0",
		"control_actions": "drain,resume,code_update",
		"held":            "inference",
		"control":         `{"id":"x","action":"drain","state":"done","at":1}`,
	})
	if f.Version != "0.4.0" || !f.Supports(ActionDrain) || f.Supports(ActionWorkerUpdate) {
		t.Errorf("fields = %+v", f)
	}
	if len(f.Held) != 1 || f.LastCommand == nil || f.LastCommand.State != "done" {
		t.Errorf("held/last = %v %+v", f.Held, f.LastCommand)
	}
	if old := ParseWorkerFields(map[string]string{}); old.Supports(ActionDrain) || old.LastCommand != nil || old.Held != nil {
		t.Errorf("legacy worker = %+v", old)
	}
}
