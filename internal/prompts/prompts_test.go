package prompts

import "testing"

func TestDefaultsRender(t *testing.T) {
	r, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ResearchSystem, ResearchUser, SummarizeSystem, SummarizeUser, ScriptSystem, ScriptUser, CriticSystem, CriticUser} {
		data := map[string]any{
			"Topic": "test topic",
			"Brief": "brief text",
			"DocsSummary": "doc summary",
			"TrustedSources": "arxiv.org",
			"TargetMinutes": 17,
			"WordBudget": 2550,
			"WPM": 150,
			"PassThreshold": 0.8,
			"Script": "script text",
			"Issues": "issue one",
		}
		out, err := r.Render(name, data)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if out == "" {
			t.Fatalf("render %s produced empty output", name)
		}
	}
}

func TestOverridePriority(t *testing.T) {
	r, err := New(map[string]string{CriticSystem: "inline-{{.Topic}}"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Render(CriticSystem, map[string]any{"Topic": "X"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "inline-X\n" {
		t.Fatalf("got %q", out)
	}
}

func TestUnknownPrompt(t *testing.T) {
	r, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render("bogus", nil); err == nil {
		t.Fatal("expected error for unknown prompt")
	}
	if _, err := New(map[string]string{"bogus": "x"}, nil); err == nil {
		t.Fatal("expected error for bogus inline prompt")
	}
}
