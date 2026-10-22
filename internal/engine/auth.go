package engine

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/MichaelKinsy/PiG/ai"
)

// Credential kinds a provider can be signed in with.
const (
	KindAPIKey = ai.CredentialAPIKey
	KindOAuth  = ai.CredentialOAuth
)

// Method is one way of signing in to a provider.
type Method struct {
	// Name is PiG's name of the method, as "OpenAI (ChatGPT subscription)".
	Name string
	// Label is the button text a provider asks for, as "Sign in with
	// ChatGPT"; empty for most.
	Label string
	// Subscription is an OAuth sign-in backed by a paid plan.
	Subscription bool
}

// AuthState is where a provider's credential comes from.
type AuthState struct {
	Configured bool
	// Source is "stored" (auth.json), "environment", "runtime" or one of
	// PiG's models.json sources.
	Source string
	// Label names an environment variable for Source "environment".
	Label string
	// Kind of the stored credential: KindAPIKey or KindOAuth.
	Kind ai.CredentialType
}

// Provider describes an inference provider and how to sign in to it.
type Provider struct {
	ID     string
	Name   string
	APIKey *Method
	OAuth  *Method
	State  AuthState
	Models int
	// Custom is defined in models.json, as an endpoint the user added.
	Custom bool
}

// Providers lists every provider PiG knows, built in and custom, in PiG's
// order.
func (e *Engine) Providers(ctx context.Context) []Provider {
	svcs := e.services()
	mr := svcs.ModelRuntime()
	kinds := map[string]ai.CredentialType{}
	if infos, err := mr.ListCredentials(ctx); err == nil {
		for _, info := range infos {
			kinds[info.ProviderID] = info.Type
		}
	}
	custom := map[string]bool{}
	if list, err := e.CustomProviders(); err == nil {
		for _, p := range list {
			custom[p.ID] = true
		}
	}
	var out []Provider
	for _, p := range mr.GetProviders() {
		info := Provider{ID: p.ID, Name: p.Name, Custom: custom[p.ID]}
		if info.Name == "" {
			info.Name = p.ID
		}
		if p.Auth.APIKey != nil {
			info.APIKey = &Method{Name: p.Auth.APIKey.Name}
		}
		if p.Auth.OAuth != nil {
			info.OAuth = &Method{Name: p.Auth.OAuth.Name, Label: p.Auth.OAuth.LoginLabel, Subscription: p.Auth.OAuth.IsSubscription}
		}
		if p.GetModels != nil {
			if models, err := p.GetModels(); err == nil {
				info.Models = len(models)
			}
		}
		st := mr.GetProviderAuthStatus(p.ID)
		info.State = AuthState{Configured: st.Configured, Source: string(st.Source), Label: st.Label, Kind: kinds[p.ID]}
		if info.State.Kind == "" && st.Source == ai.AuthSourceStored {
			info.State.Kind = KindAPIKey
		}
		out = append(out, info)
	}
	return out
}

// SubscriptionProviders are the providers with an OAuth sign-in, the
// subscriptions first, then by name.
func SubscriptionProviders(all []Provider) []Provider {
	var out []Provider
	for _, p := range all {
		if p.OAuth != nil {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OAuth.Subscription != out[j].OAuth.Subscription {
			return out[i].OAuth.Subscription
		}
		return rank(out[i].ID) < rank(out[j].ID)
	})
	return out
}

// featured are the providers shown first, in this order.
var featured = []string{"openai", "anthropic", "github-copilot", "google", "openrouter", "deepseek", "xai", "kimi-coding", "moonshotai", "zai", "minimax", "mistral", "groq"}

func rank(id string) int {
	for i, f := range featured {
		if f == id {
			return i
		}
	}
	return len(featured)
}

// APIKeyProviders are the providers that take an API key, the configured
// and featured ones first.
func APIKeyProviders(all []Provider) []Provider {
	var out []Provider
	for _, p := range all {
		if p.APIKey != nil || p.Custom {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.State.Configured != b.State.Configured {
			return a.State.Configured
		}
		if ra, rb := rank(a.ID), rank(b.ID); ra != rb {
			return ra < rb
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// Prompt is a question a sign-in asks: a text, a secret, a code pasted
// from a browser, or a choice.
type Prompt struct {
	// Kind is "text", "secret", "manual_code" or "select".
	Kind        string
	Message     string
	Placeholder string
	Options     []PromptOption
}

// PromptOption is a choice of a "select" prompt.
type PromptOption struct {
	ID, Label, Description string
}

// Notice is what a sign-in tells as it goes.
type Notice struct {
	// Kind is "info", "auth_url", "device_code" or "progress".
	Kind    string
	Message string
	// URL is the page to open, for "auth_url" and "device_code".
	URL string
	// Code is the code to enter there, for "device_code".
	Code  string
	Links []Link
}

// Link is a link a sign-in mentions.
type Link struct{ URL, Label string }

// Interaction answers a sign-in's prompts and shows its notices. Prompt
// blocks until the user answers; it returns ctx.Err() when ctx ends.
type Interaction struct {
	Prompt func(ctx context.Context, p Prompt) (string, error)
	Notify func(n Notice)
}

func (in Interaction) native() ai.AuthInteraction {
	return ai.AuthInteraction{
		Prompt: func(ctx context.Context, p ai.AuthPrompt) (string, error) {
			if in.Prompt == nil {
				return "", errors.New("此登录方式需要输入")
			}
			return in.Prompt(ctx, promptOf(p))
		},
		Notify: func(ev ai.AuthEvent) {
			if in.Notify != nil {
				in.Notify(noticeOf(ev))
			}
		},
	}
}

func promptOf(p ai.AuthPrompt) Prompt {
	switch p := p.(type) {
	case ai.AuthTextPrompt:
		return Prompt{Kind: "text", Message: p.Message, Placeholder: p.Placeholder}
	case ai.AuthSecretPrompt:
		return Prompt{Kind: "secret", Message: p.Message, Placeholder: p.Placeholder}
	case ai.AuthManualCodePrompt:
		return Prompt{Kind: "manual_code", Message: p.Message, Placeholder: p.Placeholder}
	case ai.AuthSelectPrompt:
		out := Prompt{Kind: "select", Message: p.Message}
		for _, o := range p.Options {
			out.Options = append(out.Options, PromptOption{ID: o.ID, Label: o.Label, Description: o.Description})
		}
		return out
	}
	return Prompt{Kind: p.Type()}
}

func noticeOf(ev ai.AuthEvent) Notice {
	switch ev := ev.(type) {
	case ai.AuthInfoEvent:
		n := Notice{Kind: "info", Message: ev.Message}
		for _, l := range ev.Links {
			n.Links = append(n.Links, Link{URL: l.URL, Label: l.Label})
		}
		return n
	case ai.AuthURLEvent:
		return Notice{Kind: "auth_url", URL: ev.URL, Message: ev.Instructions}
	case ai.AuthDeviceCodeEvent:
		return Notice{Kind: "device_code", URL: ev.VerificationURI, Code: ev.UserCode}
	case ai.AuthProgressEvent:
		return Notice{Kind: "progress", Message: ev.Message}
	}
	return Notice{Kind: ev.Type()}
}

// Login signs in to a provider with kind, KindOAuth or KindAPIKey,
// answering its prompts through in, and saves the credential to auth.json.
// It blocks until the sign-in finishes, fails or ctx ends.
func (e *Engine) Login(ctx context.Context, providerID string, kind ai.CredentialType, in Interaction) error {
	_, err := e.services().ModelRuntime().Login(ctx, providerID, kind, in.native())
	if err == nil {
		e.notify()
	}
	return err
}

// SaveAPIKey signs in to a provider with key, the answer to its first
// secret prompt; a provider that asks more, as Cloudflare's account ID,
// asks it through in.
func (e *Engine) SaveAPIKey(ctx context.Context, providerID, key string, in Interaction) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("请输入 API Key")
	}
	used := false
	ask := in.Prompt
	in.Prompt = func(ctx context.Context, p Prompt) (string, error) {
		if p.Kind == "secret" && !used {
			used = true
			return key, nil
		}
		if ask == nil {
			return "", errors.New(p.Message)
		}
		return ask(ctx, p)
	}
	return e.Login(ctx, providerID, KindAPIKey, in)
}

// Logout removes the stored credential of a provider. A key in the
// environment stays.
func (e *Engine) Logout(ctx context.Context, providerID string) error {
	err := e.services().ModelRuntime().Logout(ctx, providerID)
	if err == nil {
		e.notify()
	}
	return err
}
