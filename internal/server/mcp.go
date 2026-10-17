package server

import (
	"context"
	"net/http"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP tools act on a dashboard session's graph, so an agent and the UI
// can look at the same run. Each call takes the session lock, and a session
// with no run yet gets the poisoned scenario recorded first.

const sourceHint = " The scenario's poisoned source is partner-blog/policy-update."

func (s *Server) mcpHandler() http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "kintsugi", Version: "v1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "blast_radius",
		Description: "Every action downstream of a bad source, across agents and handoffs, found by one Cypher traversal over the provenance graph." + sourceHint,
	}, s.blastTool)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
}

// withRun locks the session's graph, records the scenario if the graph has
// no actions, and calls f with it.
func (s *Server) withRun(ctx context.Context, session string, f func(*falkordb.Graph) error) error {
	g, release, err := s.acquire(session, true)
	if err != nil {
		return err
	}
	defer release()
	res, err := rows(g, store.Counts, nil)
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
