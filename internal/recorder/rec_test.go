package recorder_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func run(t *testing.T, g *falkordb.Graph, agents map[string]recorder.AgentFunc, roots ...string) error {
	t.Helper()
	q := recorder.NewQueue(g, agents)
	for _, a := range roots {
		q.Enqueue(recorder.Job{Agent: a, TicketID: "t1", Payload: json.RawMessage(`{}`)})
	}
	return q.Run(context.Background())
}

func scalar(t *testing.T, g *falkordb.Graph, q string) any {
	t.Helper()
	res, err := g.Query(q, nil, nil)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	if !res.Next() {
		t.Fatalf("%s: no rows", q)
	}
	v, _ := res.Record().GetByIndex(0)
	return v
}

func count(t *testing.T, g *falkordb.Graph, q string) int64 {
	t.Helper()
	return scalar(t, g, q).(int64)
}

func TestGetSetFeeds(t *testing.T) {
	g := storetest.Graph(t)
	agents := map[string]recorder.AgentFunc{
		"w": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			if err := r.Set("k", "v1"); err != nil {
				return err
			}
			return r.Handoff("r", map[string]string{})
		},
		"r": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, ok, err := r.Get("k")
			if err != nil || !ok || v != "v1" {
				t.Errorf("Get = %q %v %v", v, ok, err)
			}
			return err
		},
	}
	if err := run(t, g, agents, "w"); err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"FEEDS":      "MATCH (:Action {tool:'w'})-[:FEEDS]->(:Action {tool:'r'}) RETURN count(*)",
		"READ":       "MATCH (:Action {tool:'r'})-[:READ]->(:Fact {key:'k', version:1}) RETURN count(*)",
		"HANDED_OFF": "MATCH (:Action {tool:'w'})-[:HANDED_OFF]->(:Action {tool:'r', ticket_id:'t1'}) RETURN count(*)",
		"seq order":  "MATCH (w:Action {tool:'w'}), (r:Action {tool:'r'}) WHERE r.seq > w.seq RETURN count(*)",
		"RAN":        "MATCH (:Agent {id:'r'})-[:RAN]->(:Action {tool:'r'}) RETURN count(*)",
	}
	for name, q := range checks {
		if n := count(t, g, q); n != 1 {
			t.Errorf("%s: got %d, want 1", name, n)
		}
	}
}

func TestVersionChain(t *testing.T) {
	g := storetest.Graph(t)
	n := 0
	var got string
	agents := map[string]recorder.AgentFunc{
		"w": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			n++
			return r.Set("k", map[int]string{1: "first", 2: "second"}[n])
		},
		"r": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, _, err := r.Get("k")
			got = v
			return err
		},
	}
	if err := run(t, g, agents, "w", "w", "r"); err != nil {
		t.Fatal(err)
	}
	if c := count(t, g, "MATCH (:Fact {key:'k', version:1})-[:NEXT]->(:Fact {key:'k', version:2}) RETURN count(*)"); c != 1 {
		t.Errorf("NEXT edges: %d", c)
	}
	if got != "second" {
		t.Errorf("Get = %q, want second", got)
	}
}

func TestValueRoundTrip(t *testing.T) {
	g := storetest.Graph(t)
	want := "say \"hi\"\nback\\slash 'single'"
	var got string
	agents := map[string]recorder.AgentFunc{
		"w": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error { return r.Set("k", want) },
		"r": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, _, err := r.Get("k")
			got = v
			return err
		},
	}
	if err := run(t, g, agents, "w", "r"); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGetMissing(t *testing.T) {
	g := storetest.Graph(t)
	agents := map[string]recorder.AgentFunc{
		"r": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, ok, err := r.Get("nope")
			if ok || err != nil || v != "" {
				t.Errorf("Get = %q %v %v", v, ok, err)
			}
			return nil
		},
	}
	if err := run(t, g, agents, "r"); err != nil {
		t.Fatal(err)
	}
	if c := count(t, g, "MATCH ()-[e:READ]->() RETURN count(e)"); c != 0 {
		t.Errorf("READ edges: %d", c)
	}
}

func TestIngestAndEffect(t *testing.T) {
	g := storetest.Graph(t)
	agents := map[string]recorder.AgentFunc{
		"a": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			if err := r.Ingest("help/x", "web", "https://example.test/x"); err != nil {
				return err
			}
			return r.Effect("refund", 12.5)
		},
	}
	if err := run(t, g, agents, "a"); err != nil {
		t.Fatal(err)
	}
	if c := count(t, g, "MATCH (:Action)-[:INGESTED]->(:Source {id:'help/x', kind:'web', trust:'ok'}) RETURN count(*)"); c != 1 {
		t.Errorf("INGESTED: %d", c)
	}
	if c := count(t, g, "MATCH (:Action)-[:CAUSED]->(:Effect {kind:'refund', amount:12.5, ticket_id:'t1', sent:true, live:true}) RETURN count(*)"); c != 1 {
		t.Errorf("CAUSED: %d", c)
	}
}

func TestFinishHash(t *testing.T) {
	g := storetest.Graph(t)
	agents := map[string]recorder.AgentFunc{
		"a": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			return r.Finish(map[string]any{"b": 1, "a": "x"})
		},
	}
	if err := run(t, g, agents, "a"); err != nil {
		t.Fatal(err)
	}
	want := `{"a":"x","b":1}`
	sum := sha256.Sum256([]byte(want))
	if got := scalar(t, g, "MATCH (a:Action) RETURN a.output"); got != want {
		t.Errorf("output = %v", got)
	}
	if got := scalar(t, g, "MATCH (a:Action) RETURN a.output_hash"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("output_hash = %v", got)
	}
	if got := scalar(t, g, "MATCH (a:Action) RETURN a.input_hash"); got == "" || got == nil {
		t.Error("no input_hash")
	}
}

var errBoom = errors.New("boom")

func TestRunStopsOnError(t *testing.T) {
	g := storetest.Graph(t)
	agents := map[string]recorder.AgentFunc{
		"a": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error { return errBoom },
	}
	if err := run(t, g, agents, "a", "a"); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
	if c := count(t, g, "MATCH (a:Action) RETURN count(a)"); c != 1 {
		t.Errorf("actions: %d, want 1", c)
	}
}
