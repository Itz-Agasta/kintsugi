// Package server is the HTTP API for the UI: one graph per visitor, a live
// scenario run and a live repair over Server-Sent Events.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

type Config struct {
	CleanGraph   string // shared clean run the repair is compared against
	Prefix       string // visitor graphs are Prefix + session id
	RunPace      time.Duration
	RepairPace   time.Duration
	Idle         time.Duration // a session unused this long is dropped
	MaxSessions  int
	AllowOrigins []string
}

type Server struct {
	db  *falkordb.FalkorDB
	w   model.Writer
	d   model.Decider
	dr  model.Drafter
	cfg Config

	mu       sync.Mutex
	sessions map[string]*session
}

// session guards one visitor graph. Runs, repairs and resets take mu so a
// graph does one thing at a time; reads do not, so the UI can refresh the
// graph while a stream is running.
type session struct {
	mu   sync.Mutex
	last time.Time
}

func New(db *falkordb.FalkorDB, w model.Writer, d model.Decider, dr model.Drafter, cfg Config) *Server {
	return &Server{db: db, w: w, d: d, dr: dr, cfg: cfg, sessions: map[string]*session{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("GET /api/cypher", s.cypher)
	mux.HandleFunc("GET /api/run/stream", s.withSession(true, s.runStream))
	mux.HandleFunc("GET /api/graph", s.withSession(false, s.graph))
	mux.HandleFunc("GET /api/blast", s.withSession(false, s.blast))
	return s.cors(mux)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); slices.Contains(s.cfg.AllowOrigins, o) {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// The session id becomes part of a graph name, so it is checked here.
var sessionID = regexp.MustCompile(`^[a-z0-9]{8,32}$`)

// withSession resolves ?session= to its graph. If exclusive, it also takes
// the session lock for the whole request and answers 409 when it is busy.
func (s *Server) withSession(exclusive bool, h func(http.ResponseWriter, *http.Request, *falkordb.Graph)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("session")
		if !sessionID.MatchString(id) {
			http.Error(w, "session must be 8 to 32 lowercase letters or digits", http.StatusBadRequest)
			return
		}
		sess := s.session(id)
		if exclusive {
			if !sess.mu.TryLock() {
				http.Error(w, "this session is busy", http.StatusConflict)
				return
			}
			defer sess.mu.Unlock()
		}
		h(w, r, s.db.SelectGraph(s.cfg.Prefix+id))
	}
}

func (s *Server) session(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		sess = &session{}
		s.sessions[id] = sess
	}
	sess.last = time.Now()
	return sess
}

func (s *Server) cypher(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"BlastRadius":   store.BlastRadius,
		"Mask":          store.Mask,
		"MustRerun":     store.MustRerun,
		"Supersede":     store.Supersede,
		"Compensation":  store.Compensation,
		"ExplainPath":   store.ExplainPath,
		"DiffFacts":     store.DiffFacts,
		"DiffDecisions": store.DiffDecisions,
		"DiffEffects":   store.DiffEffects,
	})
}

// runStream resets the visitor graph and records the poisoned scenario,
// sending one event per action as it finishes.
func (s *Server) runStream(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	send := sse(w)
	g, err := store.Reset(s.db, g.Id)
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		return
	}
	after := func(a recorder.ActionRef) {
		send("action", map[string]any{"id": a.ID, "tool": a.Tool, "ticket_id": a.TicketID, "seq": a.Seq})
		pause(r, s.cfg.RunPace)
	}
	if err := scenario.Run(r.Context(), g, true, s.w, s.d, after); err != nil {
		send("error", map[string]string{"message": err.Error()})
		return
	}
	send("done", map[string]string{"graph": g.Id})
}

type graphNode struct {
	ID    string         `json:"id"`
	Label string         `json:"label"`
	Props map[string]any `json:"props"`
}

type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	nodes, err := rows(g, store.GraphNodes, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	edges, err := rows(g, store.GraphEdges, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := struct {
		Nodes []graphNode `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}{Nodes: []graphNode{}, Edges: []graphEdge{}}
	for _, n := range nodes {
		props := n[1].(map[string]any)
		delete(props, "payload") // full ticket JSON; the UI does not need it
		out.Nodes = append(out.Nodes, graphNode{ID: fmt.Sprint(props["id"]), Label: n[0].(string), Props: props})
	}
	for _, e := range edges {
		out.Edges = append(out.Edges, graphEdge{From: e[0].(string), Type: e[1].(string), To: e[2].(string)})
	}
	writeJSON(w, out)
}

func (s *Server) blast(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	res, err := rows(g, store.BlastRadius, map[string]any{"src": r.URL.Query().Get("source")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ids := []string{}
	for _, row := range res {
		ids = append(ids, row[0].(*falkordb.Node).Properties["id"].(string))
	}
	writeJSON(w, ids)
}

// sse switches w to an event stream and returns a function that sends one
// event and flushes it.
func sse(w http.ResponseWriter) func(event string, v any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	return func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// pause waits d, or less if the client went away.
func pause(r *http.Request, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-time.After(d):
	case <-r.Context().Done():
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func rows(g *falkordb.Graph, q string, params map[string]any) ([][]any, error) {
	res, err := g.Query(q, params, nil)
	if err != nil {
		return nil, err
	}
	var out [][]any
	for res.Next() {
		out = append(out, res.Record().Values())
	}
	return out, nil
}
