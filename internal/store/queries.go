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
