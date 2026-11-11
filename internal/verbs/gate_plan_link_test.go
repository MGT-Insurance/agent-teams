package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantPlanLinkMsg = `ateam gate: a plan-approval gate needs the plan page URL. Publish the plan (the Artifact tool in Claude Code, or ateam page publish <file> --title "<title>" in a Dispatcher run) and put the URL as the first line of --context-file. If you cannot publish, add a line starting "no plan page:" with the reason.`

// runGateLikeKong mimics kong: Validate first, Run only when Validate passes.
func runGateLikeKong(t *testing.T, g *gateKong) (err error, calls int) {
	t.Helper()
	ctx, bdCalls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	if err := g.Validate(nil); err != nil {
		return err, len(*bdCalls)
	}
	err = g.Run(ctx)
	return err, len(*bdCalls)
}

func TestGatePlanLink(t *testing.T) {
	const url = "https://example.com/plan"
	cases := []struct {
		name    string
		gate    func(t *testing.T) *gateKong
		refused bool
		// usage, when set, expects a usage error containing it (not the link refusal).
		usage string
	}{
		{"refused: structured no URL", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve plan: x", Recommendation: "Yes", Alternative: "No"}
		}, true, ""},
		{"refused: prose first line no URL", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", File: makeTempFile(t, "\n  Approve plan x?\nbody\n")}
		}, true, ""},
		{"passes: URL first line of context-file", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve plan: x", ContextFile: makeTempFile(t, url+"\nwhy")}
		}, false, ""},
		{"passes: URL in recommendation", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve plan: x", Recommendation: "Yes, see " + url}
		}, false, ""},
		{"passes: no plan page line", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve plan: x", ContextFile: makeTempFile(t, "no plan page: Codex laptop, no publisher")}
		}, false, ""},
		{"passes: Approve the plan with URL", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve the plan for x", ContextFile: makeTempFile(t, url)}
		}, false, ""},
		{"refused: Approve the plan with colon", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve the plan: x"}
		}, true, ""},
		{"refused: APPROVE PLAN uppercase", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "APPROVE PLAN x"}
		}, true, ""},
		{"refused: prose first line Approve the plan", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", File: makeTempFile(t, "Approve the plan\nbody\n")}
		}, true, ""},
		{"untouched: Approve planning is not the marker", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve planning budget"}
		}, false, ""},
		{"untouched: review kind", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "review", Decision: "Approve plan: x"}
		}, false, ""},
		{"untouched: live-test-review kind", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "live-test-review", Decision: "Approve plan: x"}
		}, false, ""},
		{"untouched: question without marker", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Which database?"}
		}, false, ""},
		{"refused: --plan without link", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Plan: true, Decision: "Ship it?"}
		}, true, ""},
		{"refused: --plan, decision does not start with Approve", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Plan: true, Decision: "Pick the design", Recommendation: "A", Alternative: "B"}
		}, true, ""},
		{"refused: --plan prose without link", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Plan: true, File: makeTempFile(t, "Which option?\nbody\n")}
		}, true, ""},
		{"passes: --plan with URL", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Plan: true, Decision: "Pick the design", ContextFile: makeTempFile(t, url)}
		}, false, ""},
		{"passes: --plan with no plan page line", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Plan: true, Decision: "Pick the design", ContextFile: makeTempFile(t, "no plan page: no publisher")}
		}, false, ""},
		{"usage error: --plan with --kind review", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "review", Plan: true, Decision: "Approve plan: x", ContextFile: makeTempFile(t, url)}
		}, false, "--plan requires --kind question"},
		{"usage error: --plan with --kind live-test-review", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "live-test-review", Plan: true, Decision: "x"}
		}, false, "--plan requires --kind question"},
		{"refused: fallback Approve one-bead plan", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve one-bead plan: x"}
		}, true, ""},
		{"refused: fallback Approve this plan", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve this plan"}
		}, true, ""},
		{"refused: fallback prose Approve the revised plan", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", File: makeTempFile(t, "\nApprove the revised plan\nbody\n")}
		}, true, ""},
		{"untouched: Approve plans", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", Decision: "Approve plans"}
		}, false, ""},
		{"untouched: plan only on a later prose line", func(t *testing.T) *gateKong {
			return &gateKong{ID: "at-1", Kind: "question", File: makeTempFile(t, "Approve the budget\nthe plan is below\n")}
		}, false, ""},
	}
	for _, runtime := range []string{"laptop", "dispatcher"} {
		for _, tc := range cases {
			t.Run(runtime+"/"+tc.name, func(t *testing.T) {
				sup := t.TempDir()
				t.Setenv("DSP_SUP", sup)
				if runtime == "dispatcher" {
					t.Setenv("DSP_BEAD", "dsp-1")
				} else {
					t.Setenv("DSP_BEAD", "")
					os.Unsetenv("DSP_BEAD")
				}
				err, calls := runGateLikeKong(t, tc.gate(t))
				_, statErr := os.Stat(filepath.Join(sup, "outbox.jsonl"))
				if tc.usage != "" {
					if err == nil || !strings.Contains(err.Error(), tc.usage) {
						t.Fatalf("want usage error %q, got %v", tc.usage, err)
					}
					if calls != 0 {
						t.Errorf("usage error made %d bd calls", calls)
					}
					return
				}
				if tc.refused {
					if err == nil || !strings.Contains(err.Error(), wantPlanLinkMsg) {
						t.Fatalf("want frozen refusal, got %v", err)
					}
					if calls != 0 {
						t.Errorf("refused gate made %d bd calls", calls)
					}
					if !os.IsNotExist(statErr) {
						t.Errorf("refused gate wrote an outbox file (stat err %v)", statErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("want pass, got %v", err)
				}
			})
		}
	}
}
