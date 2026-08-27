package installer

import (
	"context"
	"testing"
)

func TestUnknownActionIsRejected(t *testing.T) {
	plan := GetPlan("run_anything")
	if plan.Supported {
		t.Fatal("unknown action must not be supported")
	}
	result := Run(context.Background(), "run_anything", nil)
	if result.Success {
		t.Fatal("unknown action must not run")
	}
}

func TestMultipassPlanIsFixed(t *testing.T) {
	plan := GetPlan(ActionInstallMultipass)
	if plan.Action != ActionInstallMultipass || plan.Publisher == "" || plan.Source == "" {
		t.Fatalf("invalid plan: %#v", plan)
	}
}

func TestVirtualBoxActionIsAllowlisted(t *testing.T) {
	plan := GetPlan(ActionInstallVirtualBox)
	if plan.Action != ActionInstallVirtualBox {
		t.Fatalf("invalid VirtualBox plan: %#v", plan)
	}
}
