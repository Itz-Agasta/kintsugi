package store

// Every Cypher query the product runs lives in this file.

// IndexQueries create the range indexes used by the recorder lookups.
var IndexQueries = []string{
	"CREATE INDEX FOR (n:Action) ON (n.id)",
	"CREATE INDEX FOR (n:Fact) ON (n.key)",
	"CREATE INDEX FOR (n:Source) ON (n.id)",
	"CREATE INDEX FOR (n:Agent) ON (n.id)",
	"CREATE INDEX FOR (n:Effect) ON (n.ticket_id)",
}

// Recorder queries. Floats go through toFloat because falkordb-go sends a
// whole float64 such as 3.0 as "3", which FalkorDB stores as an integer.

const StartAction = `
MERGE (ag:Agent {id: $agent})
CREATE (ag)-[:RAN]->(:Action {id: $id, tool: $agent, ticket_id: $ticket, seq: toFloat($seq), status: 'ok'})`

const LinkHandoff = `
MATCH (p:Action {id: $parent}), (a:Action {id: $id})
CREATE (p)-[:HANDED_OFF]->(a)`

const Ingest = `
MATCH (a:Action {id: $aid})
MERGE (s:Source {id: $sid}) ON CREATE SET s.kind = $kind, s.url = $url, s.trust = 'ok'
CREATE (a)-[:INGESTED]->(s)`

// Get reads the latest active version of a key written before the reader,
// and links the reader to it with READ and to its writer with FEEDS.
const Get = `
MATCH (f:Fact {key: $key, active: true}) WHERE f.seq < $seq
WITH f ORDER BY f.seq DESC, f.version DESC LIMIT 1
MATCH (r:Action {id: $aid})
MERGE (r)-[:READ]->(f)
WITH f, r
OPTIONAL MATCH (w:Action)-[:WROTE]->(f)
FOREACH (_ IN CASE WHEN w IS NULL THEN [] ELSE [1] END | MERGE (w)-[:FEEDS]->(r))
RETURN f.value`

const Set = `
MATCH (a:Action {id: $aid})
OPTIONAL MATCH (prev:Fact {key: $key})
WITH a, prev ORDER BY prev.version DESC LIMIT 1
CREATE (a)-[:WROTE]->(f:Fact {id: $id, key: $key, value: $value, version: coalesce(prev.version, 0) + 1, seq: a.seq, active: true})
FOREACH (_ IN CASE WHEN prev IS NULL THEN [] ELSE [1] END | CREATE (prev)-[:NEXT]->(f))`

const Effect = `
MATCH (a:Action {id: $aid})
CREATE (a)-[:CAUSED]->(:Effect {id: $id, kind: $kind, ticket_id: a.ticket_id, amount: toFloat($amount), external: true, sent: true, live: true})`

const Finish = `
MATCH (a:Action {id: $aid})
SET a.output = $output, a.output_hash = $output_hash, a.input_hash = $input_hash`

// Model response cache, in its own graph shared by all tenants.

const CacheGet = `MATCH (r:Response {key: $key}) RETURN r.body`

const CachePut = `MERGE (r:Response {key: $key}) SET r.body = $body`

// Counts summarises a graph after a run.
const Counts = `
OPTIONAL MATCH (a:Action) WITH count(a) AS actions
OPTIONAL MATCH (f:Fact) WITH actions, count(f) AS facts
OPTIONAL MATCH (e:Effect) RETURN actions, facts, count(e) AS effects`

// Repair queries (Day 2). See notes on each in the README graph model.

// BlastRadius is every action downstream of a bad source, across agents.
const BlastRadius = `
MATCH (s:Source {id: $src})<-[:INGESTED]-(a0:Action)
MATCH (a0)-[:FEEDS|HANDED_OFF*0..]->(a:Action)
RETURN DISTINCT a ORDER BY a.seq`

// Mask marks the actions that ingested the bad source and turns off the
// facts they wrote.
const Mask = `
MATCH (s:Source {id: $src})<-[:INGESTED]-(a:Action)-[:WROTE]->(f:Fact)
SET a.status = 'poisoned', f.active = false`

// ReadLatestActive is the latest active version of a key before a reader.
const ReadLatestActive = `
MATCH (f:Fact {key: $key, active: true})
WHERE f.seq < $reader_seq
RETURN f ORDER BY f.seq DESC LIMIT 1`

// LinkLateReaders links later readers of a key to a repair write, so they
// join the re-run set even if they read an older version.
const LinkLateReaders = `
MATCH (w:Action {id: $writer}), (r:Action)-[:READ]->(:Fact {key: $key})
WHERE r.seq > w.seq AND NOT r.status IN ['poisoned', 'superseded']
MERGE (w)-[:FEEDS]->(r)
RETURN r.id`

// MustRerun is the early stop: re-run only if an upstream action changed.
// FalkorDB has no EXISTS {} subquery, so this counts a pattern comprehension.
const MustRerun = `
MATCH (a:Action {id: $id})
RETURN size([(u:Action)-[:FEEDS|HANDED_OFF]->(a)
  WHERE u.status IN ['poisoned', 'rerun_changed', 'superseded', 'new'] | u]) > 0 AS must_rerun`

// Supersede retires old children that a re-run no longer hands off to, and
// everything they handed off to in turn.
const Supersede = `
MATCH (p:Action {id: $parent})-[:HANDED_OFF]->(c:Action)
WHERE NOT [c.tool, c.ticket_id] IN $new_handoffs
MATCH (c)-[:HANDED_OFF*0..]->(d:Action)
SET d.status = 'superseded'
WITH d
OPTIONAL MATCH (d)-[:WROTE]->(f:Fact) SET f.active = false
WITH d
OPTIONAL MATCH (d)-[:CAUSED]->(e:Effect) SET e.live = false, e.invalidated_by = $rid`

// Compensation lists effects that really left the system and are no longer
// what the ticket should have.
const Compensation = `
MATCH (a:Action)-[:CAUSED]->(e:Effect {sent: true, live: false, invalidated_by: $rid})
RETURN a.ticket_id, a.status, e ORDER BY a.seq`

// The three diff queries run on the clean and demo graphs and are compared
// in Go. Ids, versions and seq are never compared.

const DiffFacts = `
MATCH (f:Fact {active: true})
WITH f.key AS key, f ORDER BY f.seq DESC
WITH key, collect(f)[0] AS latest
RETURN key, latest.value ORDER BY key`

const DiffDecisions = `
MATCH (a:Action {tool: 'triage'})
WHERE NOT a.status IN ['poisoned', 'superseded']
RETURN a.ticket_id, a.output ORDER BY a.ticket_id`

const DiffEffects = `
MATCH (e:Effect {live: true})
RETURN e.ticket_id, e.kind, e.amount ORDER BY e.ticket_id, e.kind`

// ExplainPath is the shortest directed causal path from a bad source to an
// action. Only FEEDS and HANDED_OFF, never through Agent nodes.
const ExplainPath = `
MATCH p = (s:Source {id: $src})<-[:INGESTED]-(:Action)-[:FEEDS|HANDED_OFF*0..]->(a:Action {id: $aid})
RETURN p ORDER BY length(p) LIMIT 1`
