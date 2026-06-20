package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const applyExpr = `
  p:
  let
    m = p.meta or {};
    plats = m.platforms or [];
    lic = m.license or null;
    pick = x: x.shortName or (x.spdxId or (x.fullName or "?"));
    licStr =
      if lic == null then null
      else if builtins.isList lic then builtins.concatStringsSep ", " (builtins.map pick lic)
      else if builtins.isAttrs lic then pick lic
      else builtins.toString lic;
  in {
    available = m.available or true;
    darwin = builtins.elem "aarch64-darwin" plats;
    mainProgram = m.mainProgram or null;
    broken = m.broken or false;
    unfree = m.unfree or false;
    homepage = m.homepage or null;
    license = licStr;
  }
`

type colors struct {
	bold   string
	dim    string
	red    string
	green  string
	yellow string
	cyan   string
	reset  string
}

type searchEntry struct {
	Key         string
	Attr        string
	Version     string `json:"version"`
	Description string `json:"description"`
}

type packageMeta struct {
	Available   bool           `json:"available"`
	Darwin      bool           `json:"darwin"`
	MainProgram nullableString `json:"mainProgram"`
	Broken      bool           `json:"broken"`
	Unfree      bool           `json:"unfree"`
	Homepage    nullableString `json:"homepage"`
	License     nullableString `json:"license"`
}

type nullableString string

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var exitErr exitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}

		fmt.Fprintf(os.Stderr, "nixsearch: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printUsage(stdout)
		return nil
	}

	if len(args) == 1 && strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(stderr, "unknown option: %s\n", args[0])
		printUsage(stderr)
		return exitError{code: 2}
	}

	if len(args) != 1 || args[0] == "" {
		printUsage(stderr)
		return exitError{code: 2}
	}

	query := args[0]
	c := terminalColors(stdout)

	fmt.Fprintf(stderr, "%ssearching nixpkgs for %q...%s\n", c.dim, query, c.reset)

	entries, err := searchPackages(query)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Fprintf(stdout, "%s no nixpkgs package matches %s%q%s\n", noMark(c), c.bold, query, c.reset)
		fmt.Fprintf(stdout, "\n%stry instead:%s\n", c.dim, c.reset)
		fmt.Fprintf(stdout, "  - by binary name : %snix-locate bin/%s%s  %s(needs nix-index)%s\n", c.cyan, query, c.reset, c.dim, c.reset)
		fmt.Fprintf(stdout, "  - web UI         : %shttps://search.nixos.org/packages?query=%s%s\n", c.cyan, query, c.reset)
		fmt.Fprintln(stdout, "  - not in nixpkgs : write your own derivation in packages/")
		return exitError{code: 1}
	}

	fmt.Fprintf(stdout, "%s %s%d%s match(es)\n\n%sMatches:%s\n", okMark(c), c.bold, len(entries), c.reset, c.bold, c.reset)

	toDetail := make([]searchEntry, 0)
	var last searchEntry
	for _, entry := range entries {
		last = entry
		fmt.Fprintf(stdout, "  %s%-28s%s %s%s%s  %s\n", c.cyan, entry.Attr, c.reset, c.dim, entry.Version, c.reset, entry.Description)

		base := entry.Attr
		if idx := strings.LastIndexByte(base, '.'); idx >= 0 {
			base = base[idx+1:]
		}
		if base == query || entry.Attr == query {
			toDetail = append(toDetail, entry)
		}
	}

	if len(toDetail) == 0 && len(entries) == 1 {
		toDetail = append(toDetail, last)
	}

	if len(toDetail) > 0 {
		for _, entry := range toDetail {
			meta, err := evalPackage(entry.Attr)
			if err != nil {
				meta = packageMeta{}
			}
			printDetail(stdout, c, entry, meta)
		}
	} else {
		fmt.Fprintf(stdout, "\n%stip:%s run %snixsearch <exact-attr>%s for full details on one package.\n", c.dim, c.reset, c.cyan, c.reset)
	}

	fmt.Fprintf(stdout, "\n%ssource: nixpkgs flake registry (unstable) - versions may differ slightly from your flake.lock%s\n", c.dim, c.reset)
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: nixsearch <name>")
	fmt.Fprintln(w, "  Search nixpkgs for <name>; report availability + aarch64-darwin support.")
}

func searchPackages(query string) ([]searchEntry, error) {
	out, err := nixOutput("search", "nixpkgs", query, "--json")
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}

	entries, err := decodeSearchEntries(out)
	if err != nil {
		return nil, fmt.Errorf("parse nix search output: %w", err)
	}
	return entries, nil
}

func evalPackage(attr string) (packageMeta, error) {
	out, err := nixOutput("eval", "--json", "nixpkgs#"+attr, "--apply", applyExpr)
	if err != nil {
		return packageMeta{}, err
	}

	var meta packageMeta
	if err := json.Unmarshal(out, &meta); err != nil {
		return packageMeta{}, fmt.Errorf("parse nix eval output: %w", err)
	}
	return meta, nil
}

func nixOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("nix", args...)
	cmd.Stderr = io.Discard
	return cmd.Output()
}

func decodeSearchEntries(data []byte) ([]searchEntry, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected JSON object")
	}

	var entries []searchEntry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected object key")
		}

		var entry searchEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, err
		}
		entry.Key = key
		entry.Attr = strings.TrimPrefix(key, "legacyPackages.")
		if idx := strings.IndexByte(entry.Attr, '.'); idx >= 0 {
			entry.Attr = entry.Attr[idx+1:]
		}
		entries = append(entries, entry)
	}

	_, err = dec.Token()
	return entries, err
}

func (s *nullableString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = nullableString(text)
		return nil
	}

	*s = nullableString(string(data))
	return nil
}

func (s nullableString) display() string {
	if s == "" {
		return "-"
	}
	return string(s)
}

func printDetail(w io.Writer, c colors, entry searchEntry, meta packageMeta) {
	fmt.Fprintf(w, "\n%s%s%s %s(%s)%s\n", c.bold, entry.Attr, c.reset, c.dim, entry.Version, c.reset)

	if meta.Available {
		fmt.Fprintf(w, "  installable here   %s yes\n", okMark(c))
	} else {
		fmt.Fprintf(w, "  installable here   %s no (not for this system)\n", noMark(c))
	}

	if meta.Darwin {
		fmt.Fprintf(w, "  aarch64-darwin     %s in meta.platforms\n", okMark(c))
	} else {
		fmt.Fprintf(w, "  aarch64-darwin     %s not in meta.platforms\n", noMark(c))
	}

	if meta.Broken {
		fmt.Fprintf(w, "  %s! marked broken%s\n", c.yellow, c.reset)
	}
	if meta.Unfree {
		fmt.Fprintf(w, "  %s! unfree - needs nixpkgs.config.allowUnfree%s\n", c.yellow, c.reset)
	}

	fmt.Fprintf(w, "  main binary        %s\n", meta.MainProgram.display())
	fmt.Fprintf(w, "  license            %s\n", meta.License.display())
	fmt.Fprintf(w, "  homepage           %s\n", meta.Homepage.display())
	fmt.Fprintf(w, "  add to config      %s%s%s  %s<- home.packages in modules/home/default.nix%s\n", c.cyan, entry.Attr, c.reset, c.dim, c.reset)
}

func terminalColors(w io.Writer) colors {
	file, ok := w.(*os.File)
	if !ok {
		return colors{}
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return colors{}
	}
	return colors{
		bold:   "\033[1m",
		dim:    "\033[2m",
		red:    "\033[31m",
		green:  "\033[32m",
		yellow: "\033[33m",
		cyan:   "\033[36m",
		reset:  "\033[0m",
	}
}

func okMark(c colors) string {
	if c.green == "" {
		return "OK"
	}
	return c.green + "✓" + c.reset
}

func noMark(c colors) string {
	if c.red == "" {
		return "NO"
	}
	return c.red + "✗" + c.reset
}

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}
