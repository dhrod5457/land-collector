package nsdi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
)

func TestFetchExpandsURLTemplate(t *testing.T) {
	var gotPNU, gotKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPNU = r.URL.Query().Get("pnu")
		gotKey = r.URL.Query().Get("key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(time.Second, "secret key", 100, 0)
	record, err := client.Fetch(
		context.Background(),
		Endpoint{Dataset: domain.DatasetLand, URL: server.URL + "?key={serviceKey}&pnu={pnu}"},
		"1111010100100010000",
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotPNU != "1111010100100010000" || gotKey != "secret key" {
		t.Fatalf("unexpected query: pnu=%s key=%s", gotPNU, gotKey)
	}
	if string(record.PayloadJSON) != `{"ok":true}` {
		t.Fatalf("unexpected payload %s", record.PayloadJSON)
	}
}

func TestFetchRejectsNonJSONPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := NewClient(time.Second, "secret", 100, 0)
	_, err := client.Fetch(
		context.Background(),
		Endpoint{Dataset: domain.DatasetLand, URL: server.URL + "?key={serviceKey}&pnu={pnu}"},
		"1111010100100010000",
	)
	if err == nil {
		t.Fatal("expected non-JSON error")
	}
}
