package model

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Cache stores model responses by input hash in one graph shared by every
// tenant graph. Temperature 0 is not fully deterministic; the cache is what
// makes identical inputs give identical answers in the clean and demo runs.
type Cache struct{ g *falkordb.Graph }

func NewCache(g *falkordb.Graph) *Cache { return &Cache{g: g} }

func (c *Cache) Writer(inner Writer, model, promptVersion string) Writer {
	return cachedWriter{c, inner, model, promptVersion}
}

func (c *Cache) Decider(inner Decider, model, promptVersion string) Decider {
	return cachedDecider{c, inner, model, promptVersion}
}

// key hashes the model, prompt version, input content and sorted facts.
// Never ids, versions or seq: those differ between graphs.
func key(model, promptVersion string, input any, facts []KV) string {
	facts = slices.Clone(facts)
	slices.SortFunc(facts, func(a, b KV) int {
		return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Value, b.Value))
	})
	b, _ := json.Marshal([]any{model, promptVersion, input, facts})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// get fills out from the cache and reports whether it was there.
func (c *Cache) get(k string, out any) (bool, error) {
	res, err := c.g.Query(store.CacheGet, map[string]any{"key": k}, nil)
	if err != nil || !res.Next() {
		return false, err
	}
	v, _ := res.Record().GetByIndex(0)
	return true, json.Unmarshal([]byte(v.(string)), out)
}

func (c *Cache) put(k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = c.g.Query(store.CachePut, map[string]any{"key": k, "body": string(b)}, nil)
	return err
}

type cachedWriter struct {
	c             *Cache
	inner         Writer
	model, prompt string
}

func (w cachedWriter) Facts(ctx context.Context, p Page) ([]KV, error) {
	k := key(w.model, w.prompt, p, nil)
	var out []KV
	if ok, err := w.c.get(k, &out); ok || err != nil {
		return out, err
	}
	out, err := w.inner.Facts(ctx, p)
	if err != nil {
		return nil, err
	}
	return out, w.c.put(k, out)
}

type cachedDecider struct {
	c             *Cache
	inner         Decider
	model, prompt string
}

func (d cachedDecider) Decide(ctx context.Context, t Ticket, facts []KV) (Decision, error) {
	k := key(d.model, d.prompt, t, facts)
	var out Decision
	if ok, err := d.c.get(k, &out); ok || err != nil {
		return out, err
	}
	out, err := d.inner.Decide(ctx, t, facts)
	if err != nil {
		return Decision{}, err
	}
	return out, d.c.put(k, out)
}
