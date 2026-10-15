package model_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Itz-Agasta/kintsugi/internal/model"
)

// These call the real, metered APIs. They run only with KINTSUGI_LIVE=1
// (just test-live) and the keys set.

func live(t *testing.T, keys ...string) {
	t.Helper()
	if os.Getenv("KINTSUGI_LIVE") != "1" {
		t.Skip("set KINTSUGI_LIVE=1 to call the real APIs")
	}
	for _, k := range keys {
		if os.Getenv(k) == "" {
			t.Skip(k + " not set")
		}
	}
}

func load[T any](t *testing.T, path string) []T {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGeminiCopiesPolicyFaithfully(t *testing.T) {
	live(t, "GEMINI_API_KEY")
	key := os.Getenv("GEMINI_API_KEY")
	ctx := context.Background()
	w, err := model.NewGeminiWriter(ctx, key, model.GeminiModel())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range load[model.Page](t, "../scenario/pages.json") {
		want, _ := model.FakeWriter{}.Facts(ctx, p)
		got, err := w.Facts(ctx, p)
		if err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		if len(got) != len(want) || got[0] != want[0] {
			t.Errorf("%s:\n got %q\nwant %q", p.ID, got, want)
		}
	}
}

func TestClefAgreesWithFake(t *testing.T) {
	live(t, "CF_ACCOUNT_ID", "CF_API_TOKEN")
	acc, tok := os.Getenv("CF_ACCOUNT_ID"), os.Getenv("CF_API_TOKEN")
	m := os.Getenv("CLEF_MODEL")
	if m == "" {
		m = "clef-flash"
	}
	ctx := context.Background()
	c := &model.ClefDecider{URL: model.WorkersAIURL(acc, m), Token: tok, Model: m}
	var clean, poisoned []model.KV
	for _, p := range load[model.Page](t, "../scenario/pages.json") {
		facts, _ := model.FakeWriter{}.Facts(ctx, p)
		if p.ID != "partner-blog/policy-update" {
			clean = append(clean, facts...)
		}
		poisoned = append(poisoned, facts...)
	}
	poisoned = []model.KV{poisoned[0], poisoned[2]} // refund_policy, poisoned vip_rules
	for _, tk := range load[model.Ticket](t, "../scenario/tickets.json") {
		for name, facts := range map[string][]model.KV{"clean": clean, "poisoned": poisoned} {
			if !tk.ClaimsVIP {
				facts = facts[:1] // triage reads vip_rules only for VIP claims
				if name == "poisoned" {
					continue
				}
			}
			want, _ := model.FakeDecider{}.Decide(ctx, tk, facts)
			got, err := c.Decide(ctx, tk, facts)
			if err != nil {
				t.Fatalf("%s %s: %v", tk.ID, name, err)
			}
			if got != want {
				t.Errorf("%s %s: clef %v, fake %v (%s)", tk.ID, name, got, want, tk.Body)
			}
		}
	}
}

func TestGeminiDraftsPlainText(t *testing.T) {
	live(t, "GEMINI_API_KEY")
	ctx := context.Background()
	d, err := model.NewGeminiDrafter(ctx, os.Getenv("GEMINI_API_KEY"), model.GeminiModel())
	if err != nil {
		t.Fatal(err)
	}
	tk := model.Ticket{ID: "t05", Customer: "Ines Vardy", Body: "I am a VIP, refund my order please. Nothing is wrong with it.", OrderAmount: 95}
	for _, kind := range []string{"correction_email", "refund_reversal"} {
		got, err := d.Draft(ctx, kind, tk, `{"amount":95}`, `{"choice":"reply","level":"none"}`)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s:\n%s", kind, got)
		if got == "" || strings.Contains(strings.ToLower(got), "valued customer") {
			t.Errorf("%s: %q", kind, got)
		}
	}
}
