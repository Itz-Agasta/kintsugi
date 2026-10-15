// Package store connects to FalkorDB and prepares graphs.
package store

import (
	"cmp"
	"os"
	"strings"

	"github.com/FalkorDB/falkordb-go/v2"
)

// Connect opens a client from FALKORDB_HOST, FALKORDB_PORT and FALKORDB_PASSWORD.
func Connect() (*falkordb.FalkorDB, error) {
	opt := &falkordb.ConnectionOption{
		Addr: cmp.Or(os.Getenv("FALKORDB_HOST"), "localhost") + ":" + cmp.Or(os.Getenv("FALKORDB_PORT"), "6379"),
	}
	if pw := os.Getenv("FALKORDB_PASSWORD"); pw != "" {
		opt.Username = "falkordb"
		opt.Password = pw
	}
	return falkordb.FalkorDBNew(opt)
}

// Reset drops the graph if it exists and recreates its indexes.
func Reset(db *falkordb.FalkorDB, name string) (*falkordb.Graph, error) {
	g := db.SelectGraph(name)
	if err := g.Delete(); err != nil && !strings.Contains(err.Error(), "empty key") {
		return nil, err
	}
	for _, q := range IndexQueries {
		if _, err := g.Query(q, nil, nil); err != nil {
			return nil, err
		}
	}
	return g, nil
}
