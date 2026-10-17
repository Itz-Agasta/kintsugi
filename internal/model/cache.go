package model

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

func (c *Cache) Drafter(inner Drafter, model, promptVersion string) Drafter {
	return cachedDrafter{c, inner, model, promptVersion}
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

type entry struct {
	Key  string `json:"key"`
	Body string `json:"body"`
}

// Export writes every cached response to w as JSON lines, so a cache can be
// moved to another FalkorDB instance.
func (c *Cache) Export(w io.Writer) error {
	res, err := c.g.Query(store.CacheAll, nil, nil)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for res.Next() {
		r := res.Record()
		k, _ := r.GetByIndex(0)
		b, _ := r.GetByIndex(1)
		if err := enc.Encode(entry{k.(string), b.(string)}); err != nil {
			return err
		}
	}
	return nil
}

// Import reads JSON lines written by Export and returns how many it stored.
func (c *Cache) Import(r io.Reader) (int, error) {
	dec := json.NewDecoder(r)
	n := 0
	for {
		var e entry
		if err := dec.Decode(&e); err == io.EOF {
			return n, nil
		} else if err != nil {
			return n, err
		}
		if _, err := c.g.Query(store.CachePut, map[string]any{"key": e.Key, "body": e.Body}, nil); err != nil {
			return n, err
		}
		n++
	}
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

type cachedDrafter struct {
	c             *Cache
	inner         Drafter
	model, prompt string
}

func (d cachedDrafter) Draft(ctx context.Context, kind string, t Ticket, wrong, right string) (string, error) {
	k := key(d.model, d.prompt, []any{kind, t, wrong, right}, nil)
	var out string
	if ok, err := d.c.get(k, &out); ok || err != nil {
		return out, err
	}
	out, err := d.inner.Draft(ctx, kind, t, wrong, right)
	if err != nil {
		return "", err
	}
	return out, d.c.put(k, out)
}
