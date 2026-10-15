package server_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/server"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func randID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// newServer starts a server on graphs no other test or dev run uses.
func newServer(t *testing.T, tweak func(*server.Config)) (*server.Server, *httptest.Server, server.Config) {
	t.Helper()
	db := storetest.DB(t)
	cfg := server.Config{
		CleanGraph:   "test-clean-" + randID(),
		Prefix:       "test-srv-" + randID() + "-",
		MaxSessions:  20,
		Idle:         time.Hour,
		AllowOrigins: []string{"http://ui.test"},
	}
	if tweak != nil {
		tweak(&cfg)
	}
	s := server.New(db, model.FakeWriter{}, model.FakeDecider{}, model.FakeDrafter{}, cfg)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		graphs, _ := db.ListGraphs()
		for _, g := range graphs {
			if g == cfg.CleanGraph || strings.HasPrefix(g, cfg.Prefix) {
				db.SelectGraph(g).Delete()
			}
		}
	})
	return s, ts, cfg
}

type sseEvent struct {
	name string
	data json.RawMessage
}

func stream(t *testing.T, url string) []sseEvent {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%s: HTTP %d", url, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		case line == "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out
}

func names(evs []sseEvent) map[string]int {
	n := map[string]int{}
	for _, e := range evs {
		n[e.name]++
	}
	return n
}

func getJSON(t *testing.T, url string, out any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestRunStreamGraphAndBlast(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	sid := randID()
	evs := stream(t, ts.URL+"/api/run/stream?session="+sid)
	if n := names(evs); n["action"] != 66 || n["done"] != 1 || len(n) != 2 {
		t.Errorf("run events = %v, want 66 action and 1 done", n)
	}
	if evs[len(evs)-1].name != "done" {
		t.Error("last event is not done")
	}

	var g struct {
		Nodes []struct {
			ID    string         `json:"id"`
			Label string         `json:"label"`
			Props map[string]any `json:"props"`
		} `json:"nodes"`
		Edges []struct{ From, To, Type string } `json:"edges"`
	}
	if code := getJSON(t, ts.URL+"/api/graph?session="+sid, &g); code != 200 {
		t.Fatalf("graph: HTTP %d", code)
	}
	actions := 0
	for _, n := range g.Nodes {
		if n.Label == "Action" {
			actions++
			if _, ok := n.Props["payload"]; ok {
				t.Fatal("graph exposes action payload")
			}
		}
	}
	if actions != 66 || len(g.Edges) == 0 {
		t.Errorf("graph: %d actions, %d edges", actions, len(g.Edges))
	}

	var blast []string
	if code := getJSON(t, ts.URL+"/api/blast?session="+sid+"&source=partner-blog/policy-update", &blast); code != 200 || len(blast) != 25 {
		t.Errorf("blast: HTTP %d, %d ids, want 25", code, len(blast))
	}
}

func TestCypherAndHealth(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	var q map[string]string
	if code := getJSON(t, ts.URL+"/api/cypher", &q); code != 200 || !strings.Contains(q["BlastRadius"], "FEEDS|HANDED_OFF") {
		t.Errorf("cypher: HTTP %d, BlastRadius = %q", code, q["BlastRadius"])
	}
	if code := getJSON(t, ts.URL+"/healthz", nil); code != 200 {
		t.Errorf("healthz: HTTP %d", code)
	}
}

func TestBadSessionID(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	for _, id := range []string{"", "short", "UPPER123abc", "a-b-c-d-e-f", "x'})-[r]-()"} {
		if code := getJSON(t, ts.URL+"/api/graph?session="+id, nil); code != 400 {
			t.Errorf("session %q: HTTP %d, want 400", id, code)
		}
	}
}

func TestBusySessionGets409(t *testing.T) {
	_, ts, _ := newServer(t, func(c *server.Config) { c.RunPace = 20 * time.Millisecond })
	sid := randID()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/run/stream?session="+sid, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bufio.NewReader(resp.Body).ReadString('\n') // the stream has started
	if code := getJSON(t, ts.URL+"/api/run/stream?session="+sid, nil); code != 409 {
		t.Errorf("second run while busy: HTTP %d, want 409", code)
	}
}

func TestCORS(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	for origin, want := range map[string]string{"http://ui.test": "http://ui.test", "http://evil.test": ""} {
		req, _ := http.NewRequest("GET", ts.URL+"/api/cypher", nil)
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s: allow-origin %q, want %q", origin, got, want)
		}
	}
}
