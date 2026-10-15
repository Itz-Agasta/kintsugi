// Package storetest gives tests a fresh graph on the local FalkorDB.
package storetest

import (
	"crypto/rand"
	"testing"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Graph returns an empty, indexed graph that is deleted when the test ends.
func Graph(t *testing.T) *falkordb.Graph {
	t.Helper()
	db := DB(t)
	g, err := store.Reset(db, "test-"+rand.Text()[:10])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Delete() })
	return g
}

// DB connects to FalkorDB or fails the test.
func DB(t *testing.T) *falkordb.FalkorDB {
	t.Helper()
	db, err := store.Connect()
	if err != nil {
		t.Fatal(err)
	}
	return db
}
