package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A chat starts in the directory it is given, not where new chats went
// meanwhile, and its tools work there.
func TestChatStartsInItsDirectory(t *testing.T) {
	m := newMock(t, toolReply("call_1", "ls", `{"path":"."}`), textReply("好"))
	e := openEngine(t)
	addMock(t, e, m)
	first := e.CWD()
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x"), 0o644)
	if err := e.SetCWD(other); err != nil {
		t.Fatal(err)
	}
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", CWD: first})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.CWD() != first || e.CWD() != other {
		t.Fatalf("chat in %q, engine in %q", c.CWD(), e.CWD())
	}
	c.post(func() { c.Send("列一下") })
	items := settle(t, c)
	out := items[1].Parts[0].Tool.Output
	if !strings.Contains(out, "hello.txt") || strings.Contains(out, "elsewhere.txt") {
		t.Fatalf("ls ran in another directory than the chat's: %q", out)
	}

	// A directory new chats never went to gets services of its own.
	third := t.TempDir()
	c3, err := e.NewChat(ChatOptions{Model: "mock/m1", CWD: third})
	if err != nil {
		t.Fatal(err)
	}
	defer c3.Close()
	if c3.CWD() != third || e.CWD() != other {
		t.Fatalf("chat in %q, engine in %q", c3.CWD(), e.CWD())
	}
	if _, err := e.NewChat(ChatOptions{Model: "mock/m1", CWD: filepath.Join(third, "missing")}); err == nil {
		t.Fatal("a missing directory should be refused")
	}
}

// An endpoint added after new chats moved to another directory reaches
// the chats started before, with its key.
func TestEndpointReachesEarlierChats(t *testing.T) {
	m := newMock(t, textReply("好"))
	e := openEngine(t)
	addMock(t, e, m)
	q := &postQueue{}
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", Post: q.post})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := e.SetCWD(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	err = e.SaveCustomProvider(context.Background(), CustomProvider{ID: "late", BaseURL: m.URL + "/v1", API: "openai-completions", Models: []string{"x"}}, "sk-late")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetModel(Model{Ref: "late/x", Provider: "late", ID: "x", Name: "x"}); err != nil {
		t.Fatalf("the earlier chat cannot switch to the new endpoint: %v", err)
	}
	c.Send("hi")
	settleQueue(t, q, c)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) != 1 || m.requests[0]["model"] != "x" || m.auth[0] != "Bearer sk-late" {
		t.Fatalf("requests %v, auth %q", m.requests, m.auth)
	}
}

// models.json of null, or of null providers, is one without endpoints.
func TestModelsJSONNull(t *testing.T) {
	for _, content := range []string{"null", `{"providers":null}`, `{"providers":{"x":null}}`} {
		t.Run(content, func(t *testing.T) {
			e := openEngine(t)
			path := filepath.Join(e.AgentDir(), "models.json")
			ctx := context.Background()
			os.WriteFile(path, []byte(content), 0o600)
			if err := e.DeleteCustomProvider(ctx, "x"); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(path, []byte(content), 0o600)
			if err := e.SaveCustomProvider(ctx, CustomProvider{ID: "x", BaseURL: "http://127.0.0.1:1/v1", API: "openai-completions", Models: []string{"a"}}, ""); err != nil {
				t.Fatal(err)
			}
			list, err := e.CustomProviders()
			if err != nil || len(list) != 1 || list[0].ID != "x" {
				t.Fatalf("endpoints %+v, %v", list, err)
			}
		})
	}
}

// keyServer lists models to the key it is given and records the
// Authorization headers it gets.
type keyServer struct {
	*httptest.Server
	mu   sync.Mutex
	auth []string
}

func newKeyServer(t *testing.T, key string) *keyServer {
	s := &keyServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		s.mu.Lock()
		s.auth = append(s.auth, got)
		s.mu.Unlock()
		if got != "Bearer "+key {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *keyServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

// Fetching the models of an endpoint without a new key uses the saved
// one, but sends it only to the endpoint's own base URL.
func TestFetchModelsWithSavedKey(t *testing.T) {
	e := openEngine(t)
	ctx := context.Background()
	saved := newKeyServer(t, "sk-saved")
	if err := e.SaveCustomProvider(ctx, CustomProvider{ID: "gw", BaseURL: saved.URL + "/v1", API: "openai-completions", Models: []string{"m1"}}, "sk-saved"); err != nil {
		t.Fatal(err)
	}
	ids, err := e.FetchCustomModelIDs(ctx, "gw", saved.URL+"/v1/", "")
	if err != nil || strings.Join(ids, ",") != "m1" {
		t.Fatalf("with the saved key: %v, %v", ids, err)
	}
	if _, err := e.FetchCustomModelIDs(ctx, "gw", saved.URL+"/v1", "sk-other"); err == nil {
		t.Fatal("a key given replaces the saved one")
	}

	moved := newKeyServer(t, "sk-saved")
	if _, err := e.FetchCustomModelIDs(ctx, "gw", moved.URL+"/v1", ""); err == nil {
		t.Fatal("another base URL should not get the saved key")
	}
	if _, err := e.FetchCustomModelIDs(ctx, "", moved.URL+"/v1", ""); err == nil {
		t.Fatal("a new endpoint has no saved key")
	}
	if got := moved.sent(); len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Fatalf("another base URL got %q", got)
	}
	if ids, err := e.FetchCustomModelIDs(ctx, "gw", moved.URL+"/v1", "sk-saved"); err != nil || len(ids) != 1 {
		t.Fatalf("a key given goes to the new URL: %v, %v", ids, err)
	}
}
