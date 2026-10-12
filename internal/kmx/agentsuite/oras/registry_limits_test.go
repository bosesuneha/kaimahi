package oras

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Replace only registry metadata responses, leaving suite transfer and tag
// writes to the real registry fixture and ORAS client.
func oversizedRegistryIndexFixture(t *testing.T, api bool) (string, string, *testRegistry, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	fixtureServer, registry := newTestRegistryServer("", "")
	fixtureServer.Close()
	registry.referrersAPI = api
	index, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     ocispec.MediaTypeImageIndex,
		"manifests":     []any{},
		"annotations":   map[string]string{"padding": strings.Repeat("x", 1<<20)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(index) <= 1<<20 || len(index) >= 4<<20 || !json.Valid(index) {
		t.Fatal("fixture must be valid JSON between 1 MiB and the ORAS 4 MiB default")
	}
	var oversized atomic.Bool
	var indexReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiPage := api && strings.Contains(r.URL.Path, "/referrers/")
		fallbackIndex := !api && strings.Contains(r.URL.Path, "/manifests/sha256-")
		if oversized.Load() && (apiPage || fallbackIndex) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			indexReads.Add(1)
			w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
			w.Header().Set("Content-Length", strconv.Itoa(len(index)))
			w.Header().Set("Docker-Content-Digest", contentDigest(index))
			if r.Method == http.MethodGet {
				_, _ = w.Write(index)
			}
			return
		}
		registry.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	source := filepath.Join(t.TempDir(), "source")
	copyDirectory(t, filepath.Join("..", "..", "..", "..", "agentsuite", "testdata", "minimal"), source)
	return source, strings.TrimPrefix(server.URL, "http://") + "/team/suite:v1", registry, &oversized, &indexReads
}

func TestRegistryPullOversizedReferrersRemainInformational(t *testing.T) {
	for _, api := range []bool{true, false} {
		t.Run(fmt.Sprintf("api=%t", api), func(t *testing.T) {
			source, reference, _, oversized, reads := oversizedRegistryIndexFixture(t, api)
			pushed, err := PushRegistry(context.Background(), source, reference, true, false, PushOptions{})
			if err != nil {
				t.Fatal(err)
			}
			oversized.Store(true)
			output := filepath.Join(t.TempDir(), "output")
			pulled, err := PullRegistry(context.Background(), reference, output, true)
			if err != nil {
				t.Fatalf("informational provenance failure blocked suite pull: %v", err)
			}
			if pulled.Descriptor.Digest != pushed.Descriptor.Digest || pulled.Report == nil {
				t.Fatal("pull did not return the validated suite")
			}
			if _, err := os.Stat(filepath.Join(output, "agentsuite.json")); err != nil {
				t.Fatalf("suite was not extracted: %v", err)
			}
			if reads.Load() == 0 {
				t.Fatal("pull never read the oversized index")
			}
			want := "MaxMetadataBytes 1048576"
			if api {
				want = "failed to decode response: unexpected EOF"
			}
			if pulled.ProvenanceChecked || len(pulled.Provenance) != 0 || len(pulled.Warnings) != 1 || !strings.Contains(pulled.Warnings[0], want) {
				t.Fatalf("oversized index: checked=%t provenance=%+v warnings=%q, want unchecked and %q", pulled.ProvenanceChecked, pulled.Provenance, pulled.Warnings, want)
			}
		})
	}
}

func TestRegistryPushOversizedFallbackIndex(t *testing.T) {
	for _, test := range []struct {
		name        string
		require     bool
		existingTag bool
		changeSuite bool
	}{
		{"optional replacement", false, true, true},
		{"required new tag", true, false, false},
		{"required replacement", true, true, true},
		{"required same-digest repair", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, reference, registry, oversized, reads := oversizedRegistryIndexFixture(t, false)
			var oldDigest string
			if test.existingTag {
				first, err := PushRegistry(context.Background(), source, reference, true, false, PushOptions{})
				if err != nil {
					t.Fatal(err)
				}
				oldDigest = first.Descriptor.Digest.String()
			}
			if test.changeSuite {
				if err := os.WriteFile(filepath.Join(source, "extra.txt"), []byte("changed suite content"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			oversized.Store(true)
			options := PushOptions{Provenance: &ProvenanceOptions{BuilderVersion: "v1", Require: test.require}}
			pushed, err := PushRegistry(context.Background(), source, reference, true, true, options)
			registry.mu.RLock()
			current := registry.manifests[registryKey("team/suite", "v1")].digest
			registry.mu.RUnlock()
			if reads.Load() == 0 {
				t.Fatal("attachment never read the oversized fallback index")
			}
			if test.require {
				if current != oldDigest {
					t.Errorf("required attachment failure moved tag from %q to %q", oldDigest, current)
				}
				if err == nil || !strings.Contains(err.Error(), "MaxMetadataBytes 1048576") || !strings.Contains(err.Error(), "tag was not changed") {
					t.Fatalf("required push error = %v, want metadata limit refusal with unchanged tag", err)
				}
				// Once the boundary failure clears, retry must repair even if the
				// first attempt stored the provenance manifest by digest.
				oversized.Store(false)
				repaired, err := PushRegistry(context.Background(), source, reference, true, true, options)
				if err != nil || repaired.Provenance == nil || len(repaired.Warnings) != 0 {
					t.Fatalf("required retry: provenance=%+v warnings=%q error=%v", repaired.Provenance, repaired.Warnings, err)
				}
				pulled, err := PullRegistry(context.Background(), reference, filepath.Join(t.TempDir(), "repaired"), true)
				if err != nil || !pulled.ProvenanceChecked || len(pulled.Provenance) != 1 || pulled.Provenance[0].Digest != repaired.Provenance.Digest {
					t.Fatalf("repair not discoverable: checked=%t provenance=%+v error=%v", pulled.ProvenanceChecked, pulled.Provenance, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("optional attachment failure blocked push: %v", err)
			}
			if current != pushed.Descriptor.Digest.String() || current == oldDigest {
				t.Errorf("optional push did not move tag: before=%s after=%s pushed=%s", oldDigest, current, pushed.Descriptor.Digest)
			}
			if pushed.Provenance != nil || len(pushed.Warnings) != 1 || !strings.Contains(pushed.Warnings[0], "not attached") || !strings.Contains(pushed.Warnings[0], "MaxMetadataBytes 1048576") {
				t.Fatalf("optional push: provenance=%+v warnings=%q, want metadata limit warning", pushed.Provenance, pushed.Warnings)
			}
		})
	}
}
