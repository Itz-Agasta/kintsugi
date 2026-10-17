package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/repair"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP tools act on a dashboard session's graph, so an agent and the UI
// can look at the same run. Each call takes the session lock, and a session
// with no finished run gets the poisoned scenario recorded first.

const sourceHint = " The scenario's poisoned source is partner-blog/policy-update."

func (s *Server) mcpHandler() http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "kintsugi", Version: "v1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "blast_radius",
		Description: "Every action downstream of a bad source, across agents and handoffs, found by one Cypher traversal over the provenance graph." + sourceHint,
	}, s.blastTool)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explain_path",
		Description: "The shortest causal path from a bad source to one action: which action ingested the source, then each FEEDS (read what another wrote) and HANDED_OFF (agent handoff) edge." + sourceHint,
	}, s.pathTool)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "repair",
		Description: "Marks a source bad and repairs everything that depended on it: re-runs only the actions in its blast radius against clean memory, stops early where a re-run gives the same result, drafts compensation for effects that cannot be undone, and diffs the result against a clean run." + sourceHint,
	}, s.repairTool)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
}

// withRun locks the session's graph, records the scenario unless a finished
// run is already there, and calls f with it.
func (s *Server) withRun(ctx context.Context, session string, f func(*falkordb.Graph) error) error {
	g, release, err := s.acquire(session, true)
	if err != nil {
		return err
	}
	defer release()
	res, err := rows(g, store.BaselineDone, nil)
	if err != nil {
		return err
	}
	if res[0][0].(int64) == 0 {
		if g, err = store.Reset(s.db, g.Id); err != nil {
			return err
		}
		if err := scenario.Run(ctx, g, true, s.w, s.d, nil); err != nil {
			return err
		}
		if _, err := g.Query(store.MarkBaseline, nil, nil); err != nil {
			return err
		}
	}
	return f(g)
}

type sessionSource struct {
	Session string `json:"session" jsonschema:"dashboard session id, the part after demo- in the dashboard breadcrumb (8 to 32 lowercase letters or digits)"`
	Source  string `json:"source" jsonschema:"id of the source to treat as poisoned"`
}

type blastAction struct {
	ID     string  `json:"id"`
	Agent  string  `json:"agent"`
	Ticket string  `json:"ticket_id"`
	Seq    float64 `json:"seq"`
}

type blastOut struct {
	Source  string         `json:"source"`
	Count   int            `json:"count"`
	ByAgent map[string]int `json:"by_agent"`
	Actions []blastAction  `json:"actions"`
}

func (s *Server) blastTool(ctx context.Context, _ *mcp.CallToolRequest, in sessionSource) (*mcp.CallToolResult, blastOut, error) {
	out := blastOut{Source: in.Source, ByAgent: map[string]int{}, Actions: []blastAction{}}
	err := s.withRun(ctx, in.Session, func(g *falkordb.Graph) error {
		res, err := rows(g, store.BlastRadius, map[string]any{"src": in.Source})
		if err != nil {
			return err
		}
		for _, row := range res {
			p := row[0].(*falkordb.Node).Properties
			a := blastAction{ID: p["id"].(string), Agent: p["tool"].(string), Ticket: p["ticket_id"].(string), Seq: p["seq"].(float64)}
			out.Actions = append(out.Actions, a)
			out.ByAgent[a.Agent]++
		}
		out.Count = len(out.Actions)
		return nil
	})
	return nil, out, err
}

type pathIn struct {
	sessionSource
	Action string `json:"action" jsonschema:"id of the action to explain, for example one from blast_radius"`
}

type pathOut struct {
	Nodes []string    `json:"nodes"`
	Edges []graphEdge `json:"edges"`
	Path  string      `json:"path"`
}

func (s *Server) pathTool(ctx context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, pathOut, error) {
	out := pathOut{Nodes: []string{}, Edges: []graphEdge{}}
	err := s.withRun(ctx, in.Session, func(g *falkordb.Graph) error {
		res, err := rows(g, store.ExplainPath, map[string]any{"src": in.Source, "aid": in.Action})
		if err != nil {
			return err
		}
		if len(res) == 0 {
			return errors.New("no causal path from that source to that action")
		}
		for _, n := range res[0][0].([]any) {
			out.Nodes = append(out.Nodes, n.(string))
		}
		line := []string{in.Source}
		for _, e := range res[0][1].([]any) {
			e := e.([]any)
			ge := graphEdge{From: e[0].(string), Type: e[1].(string), To: e[2].(string)}
			out.Edges = append(out.Edges, ge)
			// INGESTED points from the action to the source, against the walk.
			if ge.Type == "INGESTED" {
				line = append(line, "<-INGESTED-", ge.From)
			} else {
				line = append(line, "-"+ge.Type+"->", ge.To)
			}
		}
		out.Path = strings.Join(line, " ")
		return nil
	})
	return nil, out, err
}

type repairOut struct {
	RepairID     string            `json:"repair_id"`
	Counts       map[string]int    `json:"counts"`
	Compensation []repair.CompItem `json:"compensation"`
	DiffMatch    bool              `json:"diff_match"`
	DiffLines    []string          `json:"diff_lines"`
}

func (s *Server) repairTool(ctx context.Context, _ *mcp.CallToolRequest, in sessionSource) (*mcp.CallToolResult, repairOut, error) {
	var out repairOut
	err := s.withRun(ctx, in.Session, func(g *falkordb.Graph) error {
		rep, err := repair.Run(ctx, g, in.Source, agents.All(s.w, s.d), s.dr, nil)
		if err != nil {
			return err
		}
		lines, err := repair.Diff(s.db.SelectGraph(s.cfg.CleanGraph), g)
		if err != nil {
			return err
		}
		out = repairOut{RepairID: rep.RepairID, Counts: rep.Counts, Compensation: rep.Compensation, DiffMatch: len(lines) == 0, DiffLines: append([]string{}, lines...)}
		return nil
	})
	return nil, out, err
}
