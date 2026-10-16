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
CREATE (ag)-[:RAN]->(:Action {id: $id, tool: $agent, ticket_id: $ticket, seq: toFloat($seq), status: 'ok', payload: $payload})`

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
CREATE (a)-[:CAUSED]->(:Effect {id: $id, kind: $kind, ticket_id: a.ticket_id, amount: toFloat($amount), external: true, sent: $sent, live: true})`

// DeactivateOwn turns off the versions of a key that an action wrote before,
// so a replayed write replaces them.
const DeactivateOwn = `
MATCH (:Action {id: $aid})-[:WROTE]->(f:Fact {key: $key})
SET f.active = false`

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

// Repair queries.

// BlastRadius is every action downstream of a bad source, across agents.
const BlastRadius = `
MATCH (s:Source {id: $src})<-[:INGESTED]-(a0:Action)
MATCH (a0)-[:FEEDS|HANDED_OFF*0..]->(a:Action)
RETURN DISTINCT a ORDER BY a.seq`

// Mask marks every action that ingested the bad source, whether or not it
// wrote anything, and turns off the facts it wrote.
const Mask = `
MATCH (s:Source {id: $src})<-[:INGESTED]-(a:Action)
SET a.status = 'poisoned'
WITH a
OPTIONAL MATCH (a)-[:WROTE]->(f:Fact)
FOREACH (x IN CASE WHEN f IS NULL THEN [] ELSE [f] END | SET x.active = false)`

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

// Supersede retires the old children a re-run did not hand off to again
// ($keep holds the ids it did), and everything they handed off to in turn.
const Supersede = `
MATCH (p:Action {id: $parent})-[:HANDED_OFF]->(c:Action)
WHERE NOT c.id IN $keep
MATCH (c)-[:HANDED_OFF*0..]->(d:Action)
SET d.status = 'superseded'
WITH d
OPTIONAL MATCH (d)-[:WROTE]->(f:Fact) SET f.active = false
WITH d
OPTIONAL MATCH (d)-[:CAUSED]->(e:Effect) SET e.live = false, e.invalidated_by = $rid
WITH DISTINCT d
MATCH (r:Repair {id: $rid})
MERGE (r)-[:SUPERSEDED]->(d)
RETURN d.id, d.tool, d.ticket_id, d.seq`

// Compensation lists effects that really left the system and are no longer
// what the ticket should have.
const Compensation = `
MATCH (a:Action)-[:CAUSED]->(e:Effect {sent: true, live: false, invalidated_by: $rid})
RETURN e.id, e.kind, a.ticket_id, a.status, e.amount ORDER BY a.seq`

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
// action. Only FEEDS and HANDED_OFF, never through Agent nodes. Returns the
// node ids and one [from, type, to] per edge.
const ExplainPath = `
MATCH p = (s:Source {id: $src})<-[:INGESTED]-(:Action)-[:FEEDS|HANDED_OFF*0..]->(a:Action {id: $aid})
WITH p ORDER BY length(p) LIMIT 1
RETURN [n IN nodes(p) | n.id], [r IN relationships(p) | [startNode(r).id, type(r), endNode(r).id]]`

// ActionDetail is everything the inspector shows for one action: what it read,
// wrote, handed off and caused, and the RERAN edge if a repair re-ran it.
const ActionDetail = `
MATCH (a:Action {id: $id})
RETURN properties(a),
  [(a)-[:READ]->(f:Fact) | properties(f)],
  [(a)-[:WROTE]->(f:Fact) | properties(f)],
  [(a)-[:HANDED_OFF]->(c:Action) | properties(c)],
  [(a)-[:CAUSED]->(e:Effect) | properties(e)],
  [(:Repair)-[r:RERAN]->(a) | properties(r)]`

// Repair engine bookkeeping.

// RepairCount guards against a second repair on the same graph, which the
// engine does not support: reset and run again instead.
const RepairCount = `MATCH (r:Repair) RETURN count(r)`

const CreateRepair = `CREATE (:Repair {id: $rid, source_id: $src, ts: timestamp()})`

// MarkSourceBad returns how many sources matched, so an unknown id is an error.
const MarkSourceBad = `MATCH (s:Source {id: $src}) SET s.trust = 'bad' RETURN count(s)`

const PoisonedActions = `
MATCH (:Source {id: $src})<-[:INGESTED]-(a:Action)
RETURN a.id, a.tool, a.ticket_id, a.seq`

const LoadAction = `
MATCH (a:Action {id: $id})
RETURN a.tool, a.ticket_id, a.seq, a.payload, a.status, a.output, a.output_hash`

const SetStatus = `MATCH (a:Action {id: $id}) SET a.status = $status`

// Reran records a re-run. The old output and hash stay on the RERAN edge;
// the node takes the new inputs always and the new output only when it changed.
const Reran = `
MATCH (r:Repair {id: $rid}), (a:Action {id: $id})
CREATE (r)-[:RERAN {old_output: a.output, old_hash: a.output_hash, new_hash: $output_hash}]->(a)
SET a.status = $status, a.input_hash = $input_hash
WITH a WHERE $status = 'rerun_changed'
SET a.output = $output, a.output_hash = $output_hash`

// Created records an action that exists only because a re-run handed off
// differently.
const Created = `
MATCH (r:Repair {id: $rid}), (a:Action {id: $id})
CREATE (r)-[:CREATED]->(a)
SET a.status = 'new', a.output = $output, a.output_hash = $output_hash, a.input_hash = $input_hash`

const InvalidateEffects = `
MATCH (:Action {id: $id})-[:CAUSED]->(e:Effect {live: true})
SET e.live = false, e.invalidated_by = $rid`

const OldChildren = `
MATCH (:Action {id: $id})-[:HANDED_OFF]->(c:Action)
RETURN c.id, c.tool, c.ticket_id, c.seq`

// OwnKeys lists the keys an action currently has active versions of.
const OwnKeys = `
MATCH (:Action {id: $id})-[:WROTE]->(f:Fact {active: true})
RETURN DISTINCT f.key`

const SetPayload = `MATCH (a:Action {id: $id}) SET a.payload = $payload`

// NextSeq is the smallest seq above x, so a new action can slot in before it.
const NextSeq = `MATCH (a:Action) WHERE a.seq > $seq RETURN min(a.seq)`

const StatusCounts = `
MATCH (a:Action) WHERE a.status <> 'ok'
RETURN a.status, count(a)`

// CompContext is what a follow-up draft needs: what the action did before
// repair, the ticket's current decision, and the ticket itself.
const CompContext = `
MATCH (a:Action)-[:CAUSED]->(:Effect {id: $eid})
OPTIONAL MATCH (:Repair {id: $rid})-[rr:RERAN]->(a)
WITH a, rr
MATCH (t:Action {tool: 'triage', ticket_id: a.ticket_id})
WHERE NOT t.status IN ['poisoned', 'superseded']
RETURN coalesce(rr.old_output, a.output), t.output, t.payload`

const SaveDraft = `
MATCH (e:Effect {id: $eid})
CREATE (e)-[:COMPENSATED_BY]->(:Draft {kind: $kind, text: $text})`

// Graph view for the UI. Repair bookkeeping and drafts are left out.

const GraphNodes = `
MATCH (n) WHERE n:Agent OR n:Source OR n:Action OR n:Fact OR n:Effect
RETURN labels(n)[0], properties(n)`

const GraphEdges = `
MATCH (a)-[r]->(b)
WHERE NOT a:Repair AND NOT b:Draft
RETURN a.id, type(r), b.id`

// The clean baseline gets a marker only after its run finished, so a run
// cut off halfway is rebuilt instead of trusted.

const MarkBaseline = `CREATE (:Baseline {complete: true})`

const BaselineDone = `MATCH (b:Baseline {complete: true}) RETURN count(b)`
