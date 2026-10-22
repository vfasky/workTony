// Command mygo runs go tool mygo with the build tags that leave out the
// parts of PiG the app doesn't use. GOFLAGS hands them to every go build
// mygo runs, of mygo dev as of mygo build:
//
//	go run ./scripts/mygo dev
//	go run ./scripts/mygo build -platform darwin/arm64,windows/amd64
//	go run ./scripts/mygo vet .
package main

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
)

// tags are PiG's strip tags (docs/additive-features.md of PiG), each of
// which compiles a part out of the binary.
var tags = []string{
	// The TUI's code highlighting and mermaid diagrams: the app renders
	// Markdown with its own.
	"pig_strip_syntax_highlight",
	"pig_strip_mermaid",
	// Amazon Bedrock and its AWS SDK, and Google Vertex, with their
	// providers.
	"pig_strip_bedrock_converse_stream",
	"pig_strip_google_vertex",
}

func main() {
	log.SetFlags(0)
	cmd := exec.Command("go", append([]string{"tool", "mygo"}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "GOFLAGS="+withTags(os.Getenv("GOFLAGS")))
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		log.Fatal(err)
	}
}

// withTags adds tags to the -tags of goflags, or adds a -tags.
func withTags(goflags string) string {
	fields := strings.Fields(goflags)
	for i, f := range fields {
		if v, ok := strings.CutPrefix(strings.TrimLeft(f, "-"), "tags="); ok {
			list := tags
			if v != "" {
				list = append(strings.Split(v, ","), tags...)
			}
			fields[i] = "-tags=" + strings.Join(list, ",")
			return strings.Join(fields, " ")
		}
	}
	return strings.Join(append(fields, "-tags="+strings.Join(tags, ",")), " ")
}
