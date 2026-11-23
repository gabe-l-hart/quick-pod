package decision

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSystemoneDecide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/systemone") {
			t.Errorf("path %s", r.URL.Path)
		}
		var body map[string]any
		json := readBody(r)
		_ = json
		_ = body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"answers":{"pass":{"type":"noul","noul":0.82,"confidence":0.9}},"usage":{"input_tokens":10,"output_tokens":0}}`)
	}))
	defer srv.Close()

	model := NewSystemone(srv.URL+"/v1", "/v1/systemone", "", 0.5, nil)
	res, err := model.Decide(context.Background(), Input{Topic: "test topic", ScriptSummary: "hello", CritiqueSummary: "score 0.9"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass {
		t.Fatalf("expected pass, got %+v", res)
	}
	if res.Score != 0.82 {
		t.Fatalf("score = %v", res.Score)
	}
}

func TestSystemoneFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"answers":{"pass":{"type":"noul","noul":0.31}},"usage":{"input_tokens":10,"output_tokens":0}}`)
	}))
	defer srv.Close()
	model := NewSystemone(srv.URL+"/v1", "/v1/systemone", "", 0.5, nil)
	res, err := model.Decide(context.Background(), Input{Topic: "test topic"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass {
		t.Fatalf("expected fail, got %+v", res)
	}
}

func TestSystemoneHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	}))
	defer srv.Close()
	model := NewSystemone(srv.URL+"/v1", "/v1/systemone", "", 0.5, nil)
	_, err := model.Decide(context.Background(), Input{Topic: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
}
func readBody(r *http.Request) []byte { return []byte{} }
