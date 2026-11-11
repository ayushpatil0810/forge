package permission_test

import (
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestPermissionPolicy(t *testing.T) {
	readReq := tools.ApprovalRequest{}
	editReq := tools.ApprovalRequest{Description: "edit", Diff: "-a\n+b\n"}
	bashReq := tools.ApprovalRequest{Description: "run bash"}

	// Default mode: reads free, writes and exec ask
	pDefault := permission.Policy{Mode: permission.ModeDefault}
	if pDefault.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in default mode")
	}
	if pDefault.Check("edit_file", editReq) != permission.Ask {
		t.Fatal("expected edit to ask in default mode")
	}
	if pDefault.Check("bash", bashReq) != permission.Ask {
		t.Fatal("expected exec to ask in default mode")
	}

	// Plan mode: reads free, writes and exec denied
	pPlan := permission.Policy{Mode: permission.ModePlan}
	if pPlan.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in plan mode")
	}
	if pPlan.Check("edit_file", editReq) != permission.Deny {
		t.Fatal("expected edit denied in plan mode")
	}
	if pPlan.Check("bash", bashReq) != permission.Deny {
		t.Fatal("expected exec denied in plan mode")
	}

	// AcceptEdits mode: reads free, edits free, exec asks
	pEdits := permission.Policy{Mode: permission.ModeAcceptEdits}
	if pEdits.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in accept-edits mode")
	}
	if pEdits.Check("edit_file", editReq) != permission.Allow {
		t.Fatal("expected edit allowed in accept-edits mode")
	}
	if pEdits.Check("bash", bashReq) != permission.Ask {
		t.Fatal("expected exec to ask in accept-edits mode")
	}

	// YOLO mode: everything allowed
	pYOLO := permission.Policy{Mode: permission.ModeYOLO}
	if pYOLO.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in yolo mode")
	}
	if pYOLO.Check("edit_file", editReq) != permission.Allow {
		t.Fatal("expected edit allowed in yolo mode")
	}
	if pYOLO.Check("bash", bashReq) != permission.Allow {
		t.Fatal("expected bash allowed in yolo mode")
	}
}
