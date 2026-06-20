package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeSearchEntriesPreservesOrderAndAttr(t *testing.T) {
	data := []byte(`{
		"legacyPackages.aarch64-darwin.fd": {"version": "1.0", "description": "first"},
		"legacyPackages.aarch64-darwin.python3Packages.fd": {"version": "2.0", "description": "second"}
	}`)

	entries, err := decodeSearchEntries(data)
	if err != nil {
		t.Fatalf("decodeSearchEntries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Attr != "fd" || entries[1].Attr != "python3Packages.fd" {
		t.Fatalf("attrs = %q, %q", entries[0].Attr, entries[1].Attr)
	}
	if entries[0].Version != "1.0" || entries[1].Description != "second" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestNullableStringDisplay(t *testing.T) {
	var meta packageMeta
	if err := json.Unmarshal([]byte(`{
		"available": true,
		"darwin": true,
		"mainProgram": null,
		"homepage": ["https://example.test"],
		"license": "MIT"
	}`), &meta); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got := meta.MainProgram.display(); got != "-" {
		t.Fatalf("mainProgram display = %q, want -", got)
	}
	if got := meta.Homepage.display(); got != `["https://example.test"]` {
		t.Fatalf("homepage display = %q", got)
	}
	if got := meta.License.display(); got != "MIT" {
		t.Fatalf("license display = %q, want MIT", got)
	}
}

func TestRunUnknownOption(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"--helop"}, &stdout, &stderr)

	var exitErr exitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run() error = %v, want exitError", err)
	}
	if exitErr.code != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); got != "unknown option: --helop\nUsage: nixsearch <name>\n  Search nixpkgs for <name>; report availability + aarch64-darwin support.\n" {
		t.Fatalf("stderr = %q", got)
	}
}
