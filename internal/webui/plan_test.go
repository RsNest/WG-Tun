package webui

import "testing"

func TestBuildPlanView(t *testing.T) {
	if !buildPlanView("").NoChanges || !buildPlanView("NO CHANGES").NoChanges || !buildPlanView("NO CHANGES\n").NoChanges {
		t.Fatal("empty and NO CHANGES must collapse to the NO CHANGES state")
	}
	view := buildPlanView("ADD: tunnel wg-a\nCHANGE: mapping\nDELETE: mapping\nCONFLICT: overlap\nplain")
	if view.NoChanges || len(view.Lines) != 5 {
		t.Fatalf("lines=%d none=%v", len(view.Lines), view.NoChanges)
	}
	want := []string{"add", "change", "delete", "conflict", "text"}
	for i, kind := range want {
		if view.Lines[i].Kind != kind {
			t.Fatalf("line %d kind %q want %q text %q", i, view.Lines[i].Kind, kind, view.Lines[i].Text)
		}
		if view.Lines[i].Text == "" {
			t.Fatal("plan text must be preserved")
		}
	}
}
