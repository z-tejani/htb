package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestReturnsAPIErrorsVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"100% broken"}`))
	}))
	defer server.Close()

	client := &HTBClient{
		httpClient: server.Client(),
		config: Config{
			APIBase: server.URL,
			Token:   "test-token",
		},
	}

	_, err := client.request(http.MethodGet, "/machines", nil)
	if err == nil {
		t.Fatal("expected request to fail")
	}

	if got, want := err.Error(), "100% broken"; got != want {
		t.Fatalf("unexpected error message: got %q want %q", got, want)
	}
}
