package server_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
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

// started returns a server whose clean graph is built and janitor is running.
func started(t *testing.T, tweak func(*server.Config)) (*server.Server, *httptest.Server) {
	t.Helper()
	s, ts, _ := newServer(t, tweak)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return s, ts
}

func TestRepairStreamAndPath(t *testing.T) {
	_, ts := started(t, nil)
	sid := randID()
	stream(t, ts.URL+"/api/run/stream?session="+sid)

	var notifier string
	var g struct {
		Nodes []struct {
			ID    string         `json:"id"`
			Props map[string]any `json:"props"`
		} `json:"nodes"`
	}
	getJSON(t, ts.URL+"/api/graph?session="+sid, &g)
	for _, n := range g.Nodes {
		if n.Props["tool"] == "notifier" && n.Props["ticket_id"] == "t05" {
			notifier = n.ID
		}
	}
	var path struct {
		Nodes []string                          `json:"nodes"`
		Edges []struct{ From, To, Type string } `json:"edges"`
	}
	src := "&source=partner-blog/policy-update"
	if code := getJSON(t, ts.URL+"/api/path?session="+sid+src+"&action="+notifier, &path); code != 200 || len(path.Nodes) != 5 || len(path.Edges) != 4 {
		t.Errorf("path: HTTP %d, %d nodes, %d edges, want 5 and 4", code, len(path.Nodes), len(path.Edges))
	}
	if len(path.Edges) > 0 && (path.Edges[0].Type != "INGESTED" || path.Edges[0].To != "partner-blog/policy-update") {
		t.Errorf("first edge = %+v, want INGESTED into the source", path.Edges[0])
	}

	evs := stream(t, ts.URL+"/api/repair/stream?session="+sid+src)
	if n := names(evs); n["action"] != 29 || n["report"] != 1 || n["diff"] != 1 {
		t.Errorf("repair events = %v", n)
	}
	for _, e := range evs {
		switch e.name {
		case "report":
			var rep struct {
				Compensation []any `json:"compensation"`
			}
			json.Unmarshal(e.data, &rep)
			if len(rep.Compensation) != 10 {
				t.Errorf("report: %d compensation items, want 10", len(rep.Compensation))
			}
		case "diff":
			var d struct {
				Match bool     `json:"match"`
				Lines []string `json:"lines"`
			}
			json.Unmarshal(e.data, &d)
			if !d.Match || len(d.Lines) != 0 {
				t.Errorf("diff = %+v, want a match", d)
			}
		}
	}
}

func TestRepairBeforeRunIsAnErrorEvent(t *testing.T) {
	_, ts := started(t, nil)
	evs := stream(t, ts.URL+"/api/repair/stream?session="+randID()+"&source=partner-blog/policy-update")
	if len(evs) != 1 || evs[0].name != "error" {
		t.Errorf("events = %v, want one error", names(evs))
	}
}

func TestResetDropsGraph(t *testing.T) {
	_, ts, cfg := newServer(t, nil)
	sid := randID()
	stream(t, ts.URL+"/api/run/stream?session="+sid)
	resp, err := http.Post(ts.URL+"/api/reset?session="+sid, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("reset: HTTP %d", resp.StatusCode)
	}
	if hasGraph(t, cfg.Prefix+sid) {
		t.Error("graph still exists after reset")
	}
}

func hasGraph(t *testing.T, name string) bool {
	t.Helper()
	graphs, err := storetest.DB(t).ListGraphs()
	if err != nil {
		t.Fatal(err)
	}
	return slices.Contains(graphs, name)
}

func TestCapEvictsLeastRecentlyUsed(t *testing.T) {
	_, ts, cfg := newServer(t, func(c *server.Config) { c.MaxSessions = 1 })
	first, second := randID(), randID()
	stream(t, ts.URL+"/api/run/stream?session="+first)
	stream(t, ts.URL+"/api/run/stream?session="+second)
	if hasGraph(t, cfg.Prefix+first) {
		t.Error("first session's graph survived past the cap")
	}
	if !hasGraph(t, cfg.Prefix+second) {
		t.Error("second session has no graph")
	}
}

func TestCapFullOfBusySessionsGets503(t *testing.T) {
	_, ts, _ := newServer(t, func(c *server.Config) { c.MaxSessions = 1; c.RunPace = 20 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/run/stream?session="+randID(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bufio.NewReader(resp.Body).ReadString('\n')
	if code := getJSON(t, ts.URL+"/api/graph?session="+randID(), nil); code != 503 {
		t.Errorf("new session with the only slot busy: HTTP %d, want 503", code)
	}
}

func TestSweepDropsIdleSessions(t *testing.T) {
	s, ts, cfg := newServer(t, func(c *server.Config) { c.Idle = 50 * time.Millisecond })
	sid := randID()
	stream(t, ts.URL+"/api/run/stream?session="+sid)
	time.Sleep(100 * time.Millisecond)
	s.Sweep()
	if hasGraph(t, cfg.Prefix+sid) {
		t.Error("idle session's graph was not dropped")
	}
}

func TestStartCleansUpAndBuildsClean(t *testing.T) {
	db := storetest.DB(t)
	_, _, cfg := newServer(t, nil)
	stale := db.SelectGraph(cfg.Prefix + "stale0000")
	if _, err := stale.Query("CREATE (:Action {id: 'x'})", nil, nil); err != nil {
		t.Fatal(err)
	}
	s := server.New(db, model.FakeWriter{}, model.FakeDecider{}, model.FakeDrafter{}, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if hasGraph(t, cfg.Prefix+"stale0000") {
		t.Error("stale visitor graph survived startup")
	}
	res, err := db.SelectGraph(cfg.CleanGraph).Query("MATCH (a:Action) RETURN count(a)", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Next()
	if n, _ := res.Record().GetByIndex(0); n.(int64) != 61 {
		t.Errorf("clean graph has %v actions, want 61", n)
	}
}

func TestStartRebuildsPartialClean(t *testing.T) {
	db := storetest.DB(t)
	_, _, cfg := newServer(t, nil)
	if _, err := db.SelectGraph(cfg.CleanGraph).Query("CREATE (:Action {id: 'half-done'})", nil, nil); err != nil {
		t.Fatal(err)
	}
	s := server.New(db, model.FakeWriter{}, model.FakeDecider{}, model.FakeDrafter{}, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := db.SelectGraph(cfg.CleanGraph).Query("MATCH (a:Action) RETURN count(a)", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Next()
	if n, _ := res.Record().GetByIndex(0); n.(int64) != 61 {
		t.Errorf("clean graph has %v actions after startup, want a full rebuild of 61", n)
	}
}

func TestResetReportsDeleteFailure(t *testing.T) {
	dead, err := falkordb.FalkorDBNew(&falkordb.ConnectionOption{Addr: "localhost:1"})
	if err != nil {
		t.Fatal(err)
	}
	s := server.New(dead, model.FakeWriter{}, model.FakeDecider{}, model.FakeDrafter{}, server.Config{Prefix: "x-", MaxSessions: 5, Idle: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/reset?session="+randID(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Errorf("reset with an unreachable database: HTTP %d, want 500", resp.StatusCode)
	}
}
