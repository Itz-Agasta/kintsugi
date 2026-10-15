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
