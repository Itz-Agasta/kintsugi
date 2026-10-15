// Package server is the HTTP API for the UI: one graph per visitor, a live
// scenario run and a live repair over Server-Sent Events.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/repair"
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
	mux.HandleFunc("GET /api/path", s.withSession(false, s.path))
	mux.HandleFunc("GET /api/repair/stream", s.withSession(true, s.repairStream))
	mux.HandleFunc("POST /api/reset", s.withSession(true, s.reset))
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
		sess, err := s.session(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
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

var errFull = errors.New("all demo slots are busy, try again in a minute")

// session returns the visitor's session, creating it if needed. At the cap,
// the least recently used session that is not busy makes room.
func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		if len(s.sessions) >= s.cfg.MaxSessions && !s.evictLRU() {
			return nil, errFull
		}
		sess = &session{}
		s.sessions[id] = sess
	}
	sess.last = time.Now()
	return sess, nil
}

// evictLRU drops the least recently used idle session. s.mu must be held.
func (s *Server) evictLRU() bool {
	var oldest string
	for id, sess := range s.sessions {
		if oldest == "" || sess.last.Before(s.sessions[oldest].last) {
			if sess.mu.TryLock() {
				if oldest != "" {
					s.sessions[oldest].mu.Unlock()
				}
				oldest = id
			}
		}
	}
	if oldest == "" {
		return false
	}
	s.drop(oldest)
	return true
}

// drop deletes a session and its graph. s.mu and the session lock must be
// held. The lock is left held: the session is gone, and a request still
// holding a pointer to it gets 409 instead of running on a deleted graph.
func (s *Server) drop(id string) {
	if err := deleteGraph(s.db.SelectGraph(s.cfg.Prefix + id)); err != nil {
		log.Printf("drop %s: %v", id, err)
	}
	delete(s.sessions, id)
}

// Sweep drops every session idle for longer than cfg.Idle.
func (s *Server) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if time.Since(sess.last) > s.cfg.Idle && sess.mu.TryLock() {
			s.drop(id)
		}
	}
}

// Start deletes visitor graphs left by an earlier process, builds the clean
// graph if it is empty, and runs the janitor until ctx ends.
func (s *Server) Start(ctx context.Context) error {
	graphs, err := s.db.ListGraphs()
	if err != nil {
		return err
	}
	for _, name := range graphs {
		if strings.HasPrefix(name, s.cfg.Prefix) {
			if err := deleteGraph(s.db.SelectGraph(name)); err != nil {
				return err
			}
		}
	}
	res, err := rows(s.db.SelectGraph(s.cfg.CleanGraph), "MATCH (a:Action) RETURN count(a)", nil)
	if err != nil {
		return err
	}
	if res[0][0].(int64) == 0 {
		g, err := store.Reset(s.db, s.cfg.CleanGraph)
		if err != nil {
			return err
		}
		if err := scenario.Run(ctx, g, false, s.w, s.d, nil); err != nil {
			return fmt.Errorf("building %s: %w", s.cfg.CleanGraph, err)
		}
	}
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.Sweep()
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func deleteGraph(g *falkordb.Graph) error {
	if err := g.Delete(); err != nil && !strings.Contains(err.Error(), "empty key") {
		return err
	}
	return nil
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

func (s *Server) path(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	q := r.URL.Query()
	res, err := rows(g, store.ExplainPath, map[string]any{"src": q.Get("source"), "aid": q.Get("action")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(res) == 0 {
		http.Error(w, "no causal path from that source to that action", http.StatusNotFound)
		return
	}
	out := struct {
		Nodes []string    `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}{Nodes: []string{}, Edges: []graphEdge{}}
	for _, n := range res[0][0].([]any) {
		out.Nodes = append(out.Nodes, n.(string))
	}
	for _, e := range res[0][1].([]any) {
		e := e.([]any)
		out.Edges = append(out.Edges, graphEdge{From: e[0].(string), Type: e[1].(string), To: e[2].(string)})
	}
	writeJSON(w, out)
}

// repairStream repairs the visitor graph, sending each decided action, then
// the report, then the diff against the clean run.
func (s *Server) repairStream(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	send := sse(w)
	onEvent := func(e repair.Event) {
		send("action", e)
		pause(r, s.cfg.RepairPace)
	}
	rep, err := repair.Run(r.Context(), g, r.URL.Query().Get("source"), agents.All(s.w, s.d), s.dr, onEvent)
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		return
	}
	send("report", rep)
	lines, err := repair.Diff(s.db.SelectGraph(s.cfg.CleanGraph), g)
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		return
	}
	send("diff", map[string]any{"match": len(lines) == 0, "lines": append([]string{}, lines...)})
}

// reset drops the visitor graph and forgets the session.
func (s *Server) reset(w http.ResponseWriter, r *http.Request, g *falkordb.Graph) {
	id := strings.TrimPrefix(g.Id, s.cfg.Prefix)
	s.mu.Lock()
	s.drop(id)
	s.mu.Unlock()
	writeJSON(w, map[string]bool{"reset": true})
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
