package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/MichaelKinsy/PiG/ai"

	"worktony/locales"
)

// CustomProvider is an endpoint the user added in models.json, as a local
// Ollama server or an OpenAI-compatible gateway. Its key, when it has one,
// is kept in auth.json with the other credentials.
type CustomProvider struct {
	ID      string
	Name    string
	BaseURL string
	// API is the protocol, one of CustomAPIs' IDs.
	API    string
	Models []string
}

// CustomAPI is a protocol a custom endpoint can speak.
type CustomAPI struct{ ID, Label string }

// CustomAPIs are the protocols the endpoint form offers.
var CustomAPIs = []CustomAPI{
	{"openai-completions", "OpenAI Chat Completions"},
	{"openai-responses", "OpenAI Responses"},
	{"anthropic-messages", "Anthropic Messages"},
	{"google-generative-ai", "Google Gemini"},
}

// keylessKey is the key models.json gets for an endpoint without one: PiG
// leaves out the models of a provider with no key at all, and local
// servers ignore it.
const keylessKey = "none"

var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (e *Engine) modelsPath() string { return filepath.Join(e.agentDir, "models.json") }

// modelsFile is models.json, its other members and providers kept as they
// are.
type modelsFile struct {
	rest      map[string]json.RawMessage
	providers map[string]map[string]any
}

func (e *Engine) readModels() (*modelsFile, error) {
	f := &modelsFile{rest: map[string]json.RawMessage{}, providers: map[string]map[string]any{}}
	b, err := os.ReadFile(e.modelsPath())
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(b)) == 0) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f.rest); err != nil {
		return nil, fmt.Errorf("%s: %w", locales.ErrModelsJSONInvalid(), err)
	}
	// Unmarshal takes null for no map at all: a file of null, or providers
	// of null, has none.
	if f.rest == nil {
		f.rest = map[string]json.RawMessage{}
	}
	if raw, ok := f.rest["providers"]; ok {
		if err := json.Unmarshal(raw, &f.providers); err != nil {
			return nil, fmt.Errorf("%s: %w", locales.ErrModelsJSONProviders(), err)
		}
		if f.providers == nil {
			f.providers = map[string]map[string]any{}
		}
	}
	return f, nil
}

func (e *Engine) writeModels(f *modelsFile) error {
	providers, err := json.Marshal(f.providers)
	if err != nil {
		return err
	}
	f.rest["providers"] = providers
	b, err := json.MarshalIndent(f.rest, "", "  ")
	if err != nil {
		return err
	}
	// A temporary file of its own, renamed over models.json: a reader never
	// sees half a file.
	tmp, err := os.CreateTemp(e.agentDir, ".models-*.json.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(b, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), e.modelsPath())
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// editModels runs edit over models.json and writes the result, reloads the
// registry, then runs committed, all under the lock that orders every change
// of the file and of its endpoints' keys, so changes made together build on
// each other. committed may be nil.
func (e *Engine) editModels(edit func(f *modelsFile) error, committed func() error) error {
	e.modelsMu.Lock()
	defer e.modelsMu.Unlock()
	f, err := e.readModels()
	if err != nil {
		return err
	}
	if err := edit(f); err != nil {
		return err
	}
	if err := e.writeModels(f); err != nil {
		return err
	}
	// The embedded registry's Refresh reloads models.json from disk; the
	// facade's refreshes catalogs only. The services of every directory
	// share the registry, so the chats of each see the change.
	e.services().Registry().ModelRegistry.Refresh()
	if committed != nil {
		return committed()
	}
	return nil
}

// CustomProviders lists the endpoints of models.json that have a base URL.
func (e *Engine) CustomProviders() ([]CustomProvider, error) {
	f, err := e.readModels()
	if err != nil {
		return nil, err
	}
	var out []CustomProvider
	for id, entry := range f.providers {
		base, _ := entry["baseUrl"].(string)
		if base == "" {
			continue
		}
		p := CustomProvider{ID: id, BaseURL: base}
		p.Name, _ = entry["name"].(string)
		p.API, _ = entry["api"].(string)
		if models, ok := entry["models"].([]any); ok {
			for _, m := range models {
				if m, ok := m.(map[string]any); ok {
					if id, _ := m["id"].(string); id != "" {
						p.Models = append(p.Models, id)
					}
				}
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SaveCustomProvider adds or replaces an endpoint in models.json and, when
// apiKey is not empty, stores its key in auth.json. Fields of its entry
// and models that the form does not edit are kept.
func (e *Engine) SaveCustomProvider(ctx context.Context, p CustomProvider, apiKey string) error {
	p.ID = strings.TrimSpace(strings.ToLower(p.ID))
	p.BaseURL = trimBaseURL(p.BaseURL)
	if !providerIDPattern.MatchString(p.ID) {
		return errors.New(locales.ErrEndpointID())
	}
	if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
		return errors.New(locales.ErrEndpointURL())
	}
	if !slices.ContainsFunc(CustomAPIs, func(a CustomAPI) bool { return a.ID == p.API }) {
		return errors.New(locales.ErrEndpointAPI())
	}
	var models []string
	for _, m := range p.Models {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(models, m) {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return errors.New(locales.ErrEndpointModels())
	}
	key := strings.TrimSpace(apiKey)
	creds := e.services().Credentials()
	err := e.editModels(func(f *modelsFile) error {
		entry := f.providers[p.ID]
		if entry == nil {
			for _, known := range e.services().ModelRuntime().GetProviders() {
				if known.ID == p.ID {
					return errors.New(locales.ErrEndpointBuiltIn(p.ID))
				}
			}
			entry = map[string]any{}
		}
		old := map[string]map[string]any{}
		if list, ok := entry["models"].([]any); ok {
			for _, m := range list {
				if m, ok := m.(map[string]any); ok {
					if id, _ := m["id"].(string); id != "" {
						old[id] = m
					}
				}
			}
		}
		list := make([]any, 0, len(models))
		for _, id := range models {
			m := old[id]
			if m == nil {
				m = map[string]any{"id": id}
			}
			list = append(list, m)
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = p.ID
		}
		entry["name"], entry["baseUrl"], entry["api"], entry["models"] = name, p.BaseURL, p.API, list
		if key != "" {
			// The key lives in auth.json; models.json keeps none.
			delete(entry, "apiKey")
		} else if stored, _ := creds.Read(ctx, p.ID); stored == nil {
			if _, has := entry["apiKey"]; !has {
				entry["apiKey"] = keylessKey
			}
		}
		f.providers[p.ID] = entry
		return nil
	}, func() error {
		if key == "" {
			return nil
		}
		_, err := creds.Modify(ctx, p.ID, func(*ai.Credential) (*ai.Credential, error) {
			return &ai.Credential{Type: ai.CredentialAPIKey, Key: key}, nil
		})
		return err
	})
	if err != nil {
		return err
	}
	e.notify()
	return nil
}

// DeleteCustomProvider removes an endpoint from models.json and its key
// from auth.json.
func (e *Engine) DeleteCustomProvider(ctx context.Context, id string) error {
	err := e.editModels(func(f *modelsFile) error {
		delete(f.providers, id)
		return nil
	}, func() error {
		_ = e.services().Credentials().Delete(ctx, id)
		return nil
	})
	if err != nil {
		return err
	}
	e.notify()
	return nil
}

// reloadModels reloads models.json, written by someone else.
func (e *Engine) reloadModels() {
	e.modelsMu.Lock()
	e.services().Registry().ModelRegistry.Refresh()
	e.modelsMu.Unlock()
	e.notify()
}

func trimBaseURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

// FetchCustomModelIDs asks the service at baseURL for its models, for the
// form of endpoint id, "" for a new one. It sends apiKey or, when that is
// empty, the key the endpoint is used with, but only to the base URL the
// endpoint has: a key is never sent to another service than its own.
func (e *Engine) FetchCustomModelIDs(ctx context.Context, id, baseURL, apiKey string) ([]string, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" && id != "" {
		key = e.savedKey(ctx, id, baseURL)
	}
	return FetchModelIDs(ctx, baseURL, key)
}

// savedKey is the key endpoint id is used with, as chats resolve it, when
// baseURL is the endpoint's; "" otherwise.
func (e *Engine) savedKey(ctx context.Context, id, baseURL string) string {
	list, err := e.CustomProviders()
	if err != nil {
		return ""
	}
	i := slices.IndexFunc(list, func(p CustomProvider) bool { return p.ID == id })
	if i < 0 || trimBaseURL(list[i].BaseURL) != trimBaseURL(baseURL) {
		return ""
	}
	auth, err := e.runtime.GetAuth(ctx, id)
	if err != nil || auth == nil || auth.Auth.APIKey == keylessKey {
		return ""
	}
	return auth.Auth.APIKey
}

// FetchModelIDs asks an OpenAI-compatible endpoint for its models
// (GET <baseURL>/models), for the endpoint form to fill in.
func FetchModelIDs(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimBaseURL(baseURL)+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, errors.New(locales.ErrServiceStatus(res.Status))
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s: %w", locales.ErrModelList(), err)
	}
	var ids []string
	for _, m := range body.Data {
		ids = append(ids, m.ID)
	}
	for _, m := range body.Models {
		ids = append(ids, strings.TrimPrefix(m.Name, "models/"))
	}
	sort.Strings(ids)
	return slices.Compact(ids), nil
}
