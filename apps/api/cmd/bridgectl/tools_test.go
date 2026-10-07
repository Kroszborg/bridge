package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReadRecipients(t *testing.T) {
	csv := "\uFEFFPhone, name ,order,unused\n" +
		"+919876543210,Asha,A1,x\n" +
		"\n" +
		"+919812345678,\"Ravi, Jr.\",B2,y\n"
	got, err := readRecipients(strings.NewReader(csv), "Hi {name}, order {order} ({name}) shipped. {not a var}")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	want := `[{"to":"+919876543210","vars":{"name":"Asha","order":"A1"}},{"to":"+919812345678","vars":{"name":"Ravi, Jr.","order":"B2"}}]`
	if string(raw) != want {
		t.Fatalf("got %s", raw)
	}

	for _, tc := range []struct{ csv, tmpl, err string }{
		{"", "Hi", "empty"},
		{"number,name\n+91,A\n", "Hi", "first column must be named to or phone"},
		{"to,name\n+919876543210,A\n", "Hi {first}", "no first column"},
		{"to\n", "Hi", "no recipients"},
		{"to,name\n,A\n", "Hi {name}", "line 2 has no number"},
		{"to,name\n+919876543210\n", "Hi", "wrong number of fields"},
	} {
		if _, err := readRecipients(strings.NewReader(tc.csv), tc.tmpl); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%q: got %v, want %q", tc.csv, err, tc.err)
		}
	}
}

func TestReadRecipientsWithoutVars(t *testing.T) {
	got, err := readRecipients(strings.NewReader("to\n+919876543210\n"), "Store closes early today.")
	if err != nil || len(got) != 1 || got[0].Vars != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMCPToolSchemasBuild(t *testing.T) {
	// AddTool panics when it cannot infer a tool's input schema.
	addMCPTools(mcp.NewServer(&mcp.Implementation{Name: "bridge", Version: "test"}, nil), &client{})
}

func TestOptOutPathsAreEscaped(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"Opt-out for +919876543210 not found."}}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("BRIDGE_URL", srv.URL)
	t.Setenv("BRIDGE_API_KEY", "bk_test_x")
	t.Setenv("BRIDGE_CONFIG", t.TempDir()+"/config.json")

	ctx := context.Background()
	if err := cmdOptOutNumber(ctx, "check", []string{"--json", "+919876543210"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdOptOutNumber(ctx, "remove", []string{"--json", "+919876543210"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, "|") != "GET /v1/opt-outs/+919876543210|DELETE /v1/opt-outs/+919876543210" {
		t.Fatalf("paths: %v", paths)
	}
}
