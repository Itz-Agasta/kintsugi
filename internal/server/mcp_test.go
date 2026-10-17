package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Itz-Agasta/kintsugi/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const poisoned = "partner-blog/policy-update"

// callTool calls one MCP tool on the server at url. A non-nil error is a
// protocol failure; tool failures come back as a result with IsError set.
func callTool(t *testing.T, url, name string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx := context.Background()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	return cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

// structured decodes a successful tool result into out.
func structured(t *testing.T, res *mcp.CallToolResult, err error, out any) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", text(res))
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

func text(res *mcp.CallToolResult) string {
	var s []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			s = append(s, tc.Text)
		}
	}
	return strings.Join(s, " ")
}

type blastResult struct {
	Source  string         `json:"source"`
	Count   int            `json:"count"`
	ByAgent map[string]int `json:"by_agent"`
	Actions []struct {
		ID     string  `json:"id"`
		Agent  string  `json:"agent"`
		Ticket string  `json:"ticket_id"`
		Seq    float64 `json:"seq"`
	} `json:"actions"`
}

func blastRadius(t *testing.T, url, sid string) blastResult {
	t.Helper()
	var out blastResult
	res, err := callTool(t, url, "blast_radius", map[string]any{"session": sid, "source": poisoned})
	structured(t, res, err, &out)
	return out
}

func TestMCPBlastRadiusRecordsFirst(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	sid := randID()
	out := blastRadius(t, ts.URL, sid)
	if out.Count == 0 || out.Count != len(out.Actions) {
		t.Fatalf("count %d, %d actions", out.Count, len(out.Actions))
	}
	sum := 0
	for _, n := range out.ByAgent {
		sum += n
	}
	if sum != out.Count {
		t.Errorf("by_agent sums to %d, want %d", sum, out.Count)
	}
	var want []string
	if code := getJSON(t, ts.URL+"/api/blast?session="+sid+"&source="+poisoned, &want); code != 200 {
		t.Fatalf("blast: HTTP %d", code)
	}
	var got []string
	for _, a := range out.Actions {
		got = append(got, a.ID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ids differ from /api/blast:\n got %v\nwant %v", got, want)
	}
}

func TestMCPBusySession(t *testing.T) {
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
	res, err := callTool(t, ts.URL, "blast_radius", map[string]any{"session": sid, "source": poisoned})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "busy") {
		t.Errorf("got IsError=%v %q, want a busy tool error", res.IsError, text(res))
	}
}

func TestMCPBadSession(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	res, err := callTool(t, ts.URL, "blast_radius", map[string]any{"session": "BAD", "source": poisoned})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "8 to 32") {
		t.Errorf("got IsError=%v %q, want the session rule", res.IsError, text(res))
	}
}

func TestMCPExplainPath(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	sid := randID()
	b := blastRadius(t, ts.URL, sid)
	last := b.Actions[len(b.Actions)-1].ID
	var out struct {
		Nodes []string                          `json:"nodes"`
		Edges []struct{ From, Type, To string } `json:"edges"`
		Path  string                            `json:"path"`
	}
	res, err := callTool(t, ts.URL, "explain_path", map[string]any{"session": sid, "source": poisoned, "action": last})
	structured(t, res, err, &out)
	if out.Nodes[0] != poisoned || out.Nodes[len(out.Nodes)-1] != last {
		t.Errorf("nodes %v, want %s to %s", out.Nodes, poisoned, last)
	}
	if len(out.Edges) != len(out.Nodes)-1 {
		t.Errorf("%d edges for %d nodes", len(out.Edges), len(out.Nodes))
	}
	if !strings.HasPrefix(out.Path, poisoned+" <-INGESTED- ") || !strings.HasSuffix(out.Path, " "+last) {
		t.Errorf("path %q", out.Path)
	}
}

func TestMCPExplainPathNoPath(t *testing.T) {
	_, ts, _ := newServer(t, nil)
	res, err := callTool(t, ts.URL, "explain_path", map[string]any{"session": randID(), "source": poisoned, "action": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "no causal path") {
		t.Errorf("got IsError=%v %q, want no causal path", res.IsError, text(res))
	}
}

func TestMCPRepair(t *testing.T) {
	_, ts := started(t, nil)
	sid := randID()
	var out struct {
		RepairID  string         `json:"repair_id"`
		Counts    map[string]int `json:"counts"`
		DiffMatch bool           `json:"diff_match"`
		DiffLines []string       `json:"diff_lines"`
	}
	args := map[string]any{"session": sid, "source": poisoned}
	res, err := callTool(t, ts.URL, "repair", args)
	structured(t, res, err, &out)
	if !out.DiffMatch || len(out.DiffLines) != 0 {
		t.Errorf("diff match %v, lines %v", out.DiffMatch, out.DiffLines)
	}
	if out.RepairID == "" || out.Counts["poisoned"] == 0 {
		t.Errorf("repair %q counts %v", out.RepairID, out.Counts)
	}
	// A second repair of the same session must not break the endpoint.
	if _, err := callTool(t, ts.URL, "repair", args); err != nil {
		t.Errorf("second repair: %v", err)
	}
}
