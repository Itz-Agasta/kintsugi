package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const clefAnswer = `{"model":"clef-flash","answers":{
 "choice":{"type":"choice","choice":"refund","probabilities":{"refund":0.8,"reply":0.1,"escalate":0.1}},
 "level":{"type":"score","score":1.2,"probabilities":{"0":0.1,"1":0.55,"2":0.35}}}}`

func TestClefRetriesOn429(t *testing.T) {
	calls := 0
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"result":` + clefAnswer + `,"success":true}`))
	}))
	defer srv.Close()

	c := &ClefDecider{URL: srv.URL, Token: "tok", Model: "clef-flash", Backoff: 1}
	got, err := c.Decide(context.Background(), Ticket{ID: "t1", Body: "cracked"}, []KV{{Key: "refund_policy", Value: "p"}})
	if err != nil {
		t.Fatal(err)
	}
	// Level is the highest-probability score level, not the expected score.
	if got != (Decision{Choice: "refund", Level: "partial"}) {
		t.Errorf("got %v", got)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if body["model"] != "clef-flash" {
		t.Errorf("model = %v", body["model"])
	}
}

func TestClefParsesUnwrappedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(clefAnswer))
	}))
	defer srv.Close()
	c := &ClefDecider{URL: srv.URL, Model: "clef-flash"}
	got, err := c.Decide(context.Background(), Ticket{}, nil)
	if err != nil || got.Choice != "refund" {
		t.Errorf("got %v %v", got, err)
	}
}

func TestClefReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"errors":[{"message":"bad model"}]}`))
	}))
	defer srv.Close()
	c := &ClefDecider{URL: srv.URL, Model: "clef-flash"}
	if _, err := c.Decide(context.Background(), Ticket{}, nil); err == nil {
		t.Error("want an error on HTTP 400")
	}
}

func TestClefRefundLevelIsNeverNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"answers":{"choice":{"choice":"refund"},"level":{"probabilities":{"0":0.7,"1":0.1,"2":0.2}}}}`))
	}))
	defer srv.Close()
	c := &ClefDecider{URL: srv.URL, Model: "clef-flash"}
	got, err := c.Decide(context.Background(), Ticket{}, nil)
	if err != nil || got != (Decision{Choice: "refund", Level: "full"}) {
		t.Errorf("got %v %v, want refund full (best of partial and full)", got, err)
	}
}

func TestClefRejectsMalformedAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"empty":            `{}`,
		"unknown choice":   `{"answers":{"choice":{"choice":"refund_all"}}}`,
		"refund no levels": `{"answers":{"choice":{"choice":"refund"},"level":{"probabilities":{}}}}`,
		"refund one level": `{"answers":{"choice":{"choice":"refund"},"level":{"probabilities":{"1":0.9}}}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c := &ClefDecider{URL: srv.URL, Model: "clef-flash"}
		if got, err := c.Decide(context.Background(), Ticket{}, nil); err == nil {
			t.Errorf("%s: got %v, want an error", name, got)
		}
		srv.Close()
	}
}
