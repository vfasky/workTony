//go:build pig_strip_bedrock_converse_stream && pig_strip_google_vertex

package engine

import (
	"context"
	"testing"
)

// The app's build (scripts/mygo) compiles Bedrock and Vertex out of PiG,
// whose providers then go too:
//
//	go test -tags pig_strip_bedrock_converse_stream,pig_strip_google_vertex ./internal/engine
func TestStrippedProvidersAreNotListed(t *testing.T) {
	e := openEngine(t)
	for _, p := range e.Providers(context.Background()) {
		if p.ID == "amazon-bedrock" || p.ID == "google-vertex" {
			t.Errorf("Providers lists %s", p.ID)
		}
	}
}
