package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

const sampleSmartContext = "ガイド\n<PageList>\n<Page title=\"A\">\nあいう\n</Page>\n<Page title=\"B\">\nかきく\n</Page>\n<Page title=\"C\">\nさしす\n</Page>\n</PageList>\n"

func TestTruncateSmartContext_NoTruncation(t *testing.T) {
	got, err := truncateSmartContext(sampleSmartContext, 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if got != sampleSmartContext {
		t.Errorf("expected full text, got %q", got)
	}
}

func TestTruncateSmartContext_CutsAtPageBoundary(t *testing.T) {
	// Limit falls in the middle of page B, so the cut must be at the start of page B
	pageB := strings.Index(sampleSmartContext, `<Page title="B">`)
	pageBRunes := len([]rune(sampleSmartContext[:pageB]))

	got, err := truncateSmartContext(sampleSmartContext, 0, pageBRunes+5)
	if err != nil {
		t.Fatal(err)
	}
	body, notice, found := strings.Cut(got, "\n\n[truncated:")
	if !found {
		t.Fatalf("expected truncation notice, got %q", got)
	}
	if body != sampleSmartContext[:pageB] {
		t.Errorf("unexpected body: %q", body)
	}
	if !strings.Contains(body, "<PageList>") {
		t.Errorf("<PageList> should be kept in the first chunk")
	}
	if !strings.Contains(notice, "offset=") {
		t.Errorf("notice should contain next offset: %q", notice)
	}

	// Continue from the next offset: should start at page B
	got2, err := truncateSmartContext(sampleSmartContext, pageBRunes, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got2, `<Page title="B">`) {
		t.Errorf("continuation should start at page B, got %q", got2)
	}
	if body+got2 != sampleSmartContext {
		t.Errorf("chunks should reassemble to the original text")
	}
}

func TestTruncateSmartContext_UsesLastBoundaryWithinLimit(t *testing.T) {
	pageC := strings.Index(sampleSmartContext, `<Page title="C">`)
	pageCRunes := len([]rune(sampleSmartContext[:pageC]))

	got, err := truncateSmartContext(sampleSmartContext, 0, pageCRunes+2)
	if err != nil {
		t.Fatal(err)
	}
	body, _, _ := strings.Cut(got, "\n\n[truncated:")
	if body != sampleSmartContext[:pageC] {
		t.Errorf("expected cut at page C, got %q", body)
	}
}

func TestTruncateSmartContext_FallbackPlainCut(t *testing.T) {
	text := strings.Repeat("あ", 50)
	got, err := truncateSmartContext(text, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	body, notice, found := strings.Cut(got, "\n\n[truncated:")
	if !found {
		t.Fatalf("expected truncation notice")
	}
	if body != strings.Repeat("あ", 10) {
		t.Errorf("expected 10 runes, got %q", body)
	}
	if !strings.Contains(notice, "offset=10 ") {
		t.Errorf("unexpected notice: %q", notice)
	}
}

func TestTruncateSmartContext_OffsetOutOfRange(t *testing.T) {
	if _, err := truncateSmartContext("abc", 3, 10); err == nil {
		t.Error("expected error for offset == total")
	}
	if _, err := truncateSmartContext("abc", 4, 10); err == nil {
		t.Error("expected error for offset > total")
	}
}

func newSmartContextTestTool(t *testing.T, handler http.HandlerFunc) *GetSmartContextTool {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := scrapbox.NewClient("default-project", "test-sid", server.URL+"/api", 5*time.Second)
	return NewGetSmartContextTool(client)
}

func TestGetSmartContextTool_Execute(t *testing.T) {
	var gotPath, gotTitle, gotCookie string
	tool := newSmartContextTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotTitle = r.URL.Query().Get("title")
		if c, err := r.Cookie("connect.sid"); err == nil {
			gotCookie = c.Value
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(sampleSmartContext))
	})

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"title":   "日本語 タイトル/スラッシュ",
		"hops":    float64(2),
		"project": "my-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != sampleSmartContext {
		t.Errorf("unexpected result: %v", result)
	}
	if gotPath != "/api/smart-context/export-2hop-links/my-project.txt" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotTitle != "日本語 タイトル/スラッシュ" {
		t.Errorf("unexpected title: %s", gotTitle)
	}
	if gotCookie != "test-sid" {
		t.Errorf("connect.sid cookie not sent: %q", gotCookie)
	}
}

func TestGetSmartContextTool_DefaultsToOneHopAndDefaultProject(t *testing.T) {
	var gotPath string
	tool := newSmartContextTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte("ok"))
	})

	if _, err := tool.Execute(context.Background(), map[string]interface{}{"title": "A"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/smart-context/export-1hop-links/default-project.txt" {
		t.Errorf("unexpected path: %s", gotPath)
	}
}

func TestGetSmartContextTool_InvalidArguments(t *testing.T) {
	tool := newSmartContextTestTool(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("API should not be called for invalid arguments")
	})

	cases := []map[string]interface{}{
		{},
		{"title": ""},
		{"title": "A", "hops": float64(3)},
		{"title": "A", "max_chars": float64(0)},
		{"title": "A", "max_chars": float64(maxSmartContextMaxChars + 1)},
		{"title": "A", "offset": float64(-1)},
	}
	for _, args := range cases {
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}

func TestGetSmartContextTool_HTTPErrors(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "SCRAPBOX_INVALID_INPUT"},
		{http.StatusUnauthorized, "SCRAPBOX_AUTH_FAILED"},
		{http.StatusForbidden, "SCRAPBOX_AUTH_FAILED"},
		{http.StatusNotFound, "SCRAPBOX_NOT_FOUND"},
		{http.StatusInternalServerError, "SCRAPBOX_NETWORK_ERROR"},
	}
	for _, c := range cases {
		tool := newSmartContextTestTool(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		})
		_, err := tool.Execute(context.Background(), map[string]interface{}{"title": "A"})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("status %d: expected %s, got %v", c.status, c.code, err)
		}
	}
}
