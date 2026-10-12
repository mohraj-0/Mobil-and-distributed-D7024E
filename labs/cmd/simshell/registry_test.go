package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"d7024e/metadata"
)

func registryShell(t *testing.T, nodes int) (*shell, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	pk, sk, err := metadata.GeneratePublisherKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := metadata.EncodePublisherPrivateKey(sk)
	keyfile, owners := filepath.Join(dir, "publisher.key"), filepath.Join(dir, "owners.json")
	if err := os.WriteFile(keyfile, []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"example.com": hex.EncodeToString(pk)})
	if err := os.WriteFile(owners, data, 0600); err != nil {
		t.Fatal(err)
	}
	s := &shell{}
	root := s.root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	t.Cleanup(s.close)
	if err := s.dispatch([]string{"--nodes", fmt.Sprint(nodes), "--k", fmt.Sprint(nodes), "--owners-file", owners, "--publisher-key", keyfile, "show", "nodes"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	return s, out, dir
}

func runShell(t *testing.T, s *shell, out *bytes.Buffer, args ...string) string {
	t.Helper()
	out.Reset()
	if err := s.dispatch(args); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out.String()
}

func TestUnifiedCLIPublishHistoryAndInstall(t *testing.T) {
	s, out, dir := registryShell(t, 3)
	file := filepath.Join(dir, "input.bin")
	first := []byte{0, 255, 128, 7}
	if err := os.WriteFile(file, first, 0600); err != nil {
		t.Fatal(err)
	}
	got := runShell(t, s, out, "publish", "example.com:demo:1", file)
	if strings.Count(got, " accepted") != 3 {
		t.Fatalf("replicas: %s", got)
	}
	got = runShell(t, s, out, "show", "example.com:demo")
	if strings.Count(got, "version=") != 1 || !strings.Contains(got, "version=1 record=") || !strings.Contains(got, "blob=") || !strings.Contains(got, "prev=genesis") {
		t.Fatalf("first history: %s", got)
	}
	second := []byte("second release")
	if err := os.WriteFile(file, second, 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "--prev=1", "example.com:demo:7", file)
	got = runShell(t, s, out, "show", "example.com:demo")
	if strings.Count(got, "version=") != 2 || strings.Index(got, "version=7") > strings.Index(got, "version=1") {
		t.Fatalf("multiple history: %s", got)
	}
	got = runShell(t, s, out, "show", "ds")
	for _, want := range []string{"blob bytes=", "version-record example.com:demo version=1", "version-record example.com:demo version=7", "latest-pointer example.com:demo version=7", "prev=genesis"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for version, want := range map[string][]byte{"1": first, "7": second, "latest": second} {
		filename := filepath.Join(dir, "installed-"+version)
		got = runShell(t, s, out, "install", "--output", filename, "example.com:demo:"+version)
		data, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(data, want) || !strings.Contains(got, "received from") {
			t.Fatalf("install %s: %v %s %v", version, data, got, err)
		}
	}
	// Both entry points use this same tree: REPL dispatch and Cobra Execute.
	out.Reset()
	s.root().SetArgs([]string{"show", "example.com:demo"})
	if err := s.root().Execute(); err != nil || !strings.Contains(out.String(), "verified history") {
		t.Fatalf("Cobra history: %v %s", err, out)
	}
}

func TestPublishPreflightForceAndFlagReset(t *testing.T) {
	s, out, dir := registryShell(t, 2)
	file := filepath.Join(dir, "package")
	if err := os.WriteFile(file, []byte("package"), 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "example.com:demo:1", file)
	runShell(t, s, out, "publish", "example.com:demo:2", file)
	if err := s.dispatch([]string{"publish", "--force", "--prev=0"}); err == nil {
		t.Fatal("accepted missing publication arguments")
	}
	if s.opts.force || s.opts.prev != "" {
		t.Fatal("flags leaked after argument error")
	}
	before := s.node.DataSnapshot()
	if err := s.dispatch([]string{"publish", "--prev=1", "example.com:demo:3", file}); err == nil || !strings.Contains(err.Error(), "extend the latest") {
		t.Fatalf("fork preflight: %v", err)
	}
	if len(before) != len(s.node.DataSnapshot()) {
		t.Fatal("preflight failure stored records")
	}
	out.Reset()
	if err := s.dispatch([]string{"publish", "--force", "--prev=0", "example.com:demo:3", file}); err == nil {
		t.Fatal("forced fork accepted")
	}
	if strings.Count(out.String(), " rejected:") != 2 {
		t.Fatalf("force did not reach both receivers: %s", out)
	}
	got := runShell(t, s, out, "show", "example.com:demo")
	if strings.Contains(got, "version=3") {
		t.Fatal("forced fork changed head")
	}
	// Neither --force nor --prev persists into the next interactive command.
	runShell(t, s, out, "publish", "example.com:demo:4", file)
	got = runShell(t, s, out, "show", "example.com:demo")
	if !strings.Contains(got, "version=4") || strings.Count(got, "version=") != 3 {
		t.Fatalf("flags leaked: %s", got)
	}
}

func TestShowUnknownAndMalformedRecords(t *testing.T) {
	s, out, dir := registryShell(t, 1)
	for _, args := range [][]string{{"show", "unknown.com:demo"}, {"show", "example.com:unknown"}, {"show", "dns", "unknown.com"}, {"show", "dns"}, {"show", "bad"}, {"show", "Example.com:demo"}} {
		out.Reset()
		if err := s.dispatch(args); err == nil {
			t.Fatalf("accepted unknown/malformed query %v", args)
		}
		if strings.Contains(out.String(), "verified history") {
			t.Fatal("printed partial verified history")
		}
	}
	malformed := []byte(`{"tag":"version-record","version":"bad"}`)
	if _, err := s.node.Store(malformed); err != nil {
		t.Fatal(err)
	}
	got := runShell(t, s, out, "show", "ds")
	if !strings.Contains(got, "version-record malformed") {
		t.Fatalf("malformed display: %s", got)
	}
	file := filepath.Join(dir, "package")
	if err := os.WriteFile(file, []byte("package"), 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "example.com:demo:1", file)
	p, err := s.node.LookupLatest("example.com", "demo")
	if err != nil {
		t.Fatal(err)
	}
	// Malicious/corrupt local storage must not produce authenticated output.
	s.node.DataStore[p.VersionRecordHash] = []byte(`{"tag":"version-record"}`)
	out.Reset()
	if err := s.dispatch([]string{"show", "example.com:demo"}); err == nil || out.Len() != 0 {
		t.Fatalf("corrupt history printed: %v %s", err, out)
	}
	destination := filepath.Join(dir, "existing")
	if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatch([]string{"install", "--output", destination, "example.com:demo:latest"}); err == nil {
		t.Fatal("installed corrupt history")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "keep" {
		t.Fatal("failed install overwrote destination")
	}
}

type failingOwner struct{}

func (failingOwner) ResolvePublicKey(string) (ed25519.PublicKey, error) {
	return nil, fmt.Errorf("test DNS unavailable")
}

func TestShowDNSUsesNodesVerifier(t *testing.T) {
	s, out, _ := registryShell(t, 1)
	key, err := s.node.OwnershipVerifier.ResolvePublicKey("example.com")
	if err != nil {
		t.Fatal(err)
	}
	got := runShell(t, s, out, "show", "dns", "example.com")
	if !strings.Contains(got, hex.EncodeToString(key)) {
		t.Fatalf("DNS key: %s", got)
	}
	s.node.OwnershipVerifier = failingOwner{}
	if err := s.dispatch([]string{"show", "dns", "example.com"}); err == nil || !strings.Contains(err.Error(), "test DNS unavailable") {
		t.Fatalf("lookup failure hidden: %v", err)
	}
	s.node.OwnershipVerifier = nil
	if err := s.dispatch([]string{"show", "dns", "example.com"}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("missing verifier: %v", err)
	}
}

func TestStructuredDisplayMalformedPointersAndShortKeys(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"tag":"latest-pointer"}`), []byte(`{"tag":"version-record"}`), []byte(`{"tag":"latest-pointer","version":"bad"}`)} {
		out := &bytes.Buffer{}
		displayStoredValue(out, "bad", data)
		if !strings.Contains(out.String(), "malformed") {
			t.Fatalf("accepted malformed metadata: %s", out)
		}
	}
	out := &bytes.Buffer{}
	displayStoredValue(out, "x", []byte("raw blob"))
	if !strings.Contains(out.String(), "malformed") {
		t.Fatal("ignored bad content hash")
	}
	data := []byte(`{"tag":"custom","value":1}`)
	out.Reset()
	displayStoredValue(out, hashHex(data), data)
	if !strings.Contains(out.String(), "blob bytes=") {
		t.Fatal("arbitrary JSON blob not supported")
	}
}

func TestUnifiedCLIPart1Compatibility(t *testing.T) {
	s, out, dir := registryShell(t, 2)
	file, download := filepath.Join(dir, "input"), filepath.Join(dir, "output")
	data := []byte("Part 1 content")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"ping", s.peers[1].RoutingTable.Me().Address}, {"put", file}, {"store", file}, {"get", hashHex(data), download}, {"show", "rt"}, {"show", "ds"}, {"show", "nodes"}} {
		runShell(t, s, out, args...)
	}
	found, err := os.ReadFile(download)
	if err != nil || !bytes.Equal(found, data) {
		t.Fatalf("Part 1 get: %q %v", found, err)
	}
	for _, name := range []string{"exit", "quit", "serve", "test", "publish", "install", "keygen"} {
		cmd, _, err := s.root().Find([]string{name})
		if err != nil || cmd == s.root() {
			t.Fatalf("command %s missing: %v", name, err)
		}
	}
}

func TestShowDataStoreDuringConcurrentStores(t *testing.T) {
	s, out, _ := registryShell(t, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if _, err := s.node.Store([]byte(fmt.Sprint(i))); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		runShell(t, s, out, "show", "ds")
	}
	wg.Wait()
}

func TestKeygenAndOwnerFileValidation(t *testing.T) {
	s := &shell{}
	out := &bytes.Buffer{}
	s.root().SetOut(out)
	file := filepath.Join(t.TempDir(), "publisher.key")
	if err := s.dispatch([]string{"keygen", file}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := metadata.LoadPublisherPrivateKey(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), hex.EncodeToString(sk[32:])) {
		t.Fatalf("incorrect TXT: %s", out)
	}
	if s.node != nil {
		t.Fatal("keygen unnecessarily started a node")
	}
	if err := s.dispatch([]string{"keygen", file}); err == nil {
		t.Fatal("keygen overwrote key")
	}
	if _, err := loadOwners(""); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"invalid JSON", `{"example.com":"bad"}`, `{"Example.com":"` + hex.EncodeToString(sk[32:]) + `"}`} {
		owners := filepath.Join(t.TempDir(), "owners.json")
		if err := os.WriteFile(owners, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadOwners(owners); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestOwnershipEnvironmentAndWindowsJSON(t *testing.T) {
	setup, _, dir := registryShell(t, 1)
	data, err := os.ReadFile(setup.opts.ownersFile)
	if err != nil {
		t.Fatal(err)
	}
	bomFile := filepath.Join(dir, "windows-owners.json")
	if err := os.WriteFile(bomFile, append([]byte{0xef, 0xbb, 0xbf}, data...), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OWNERS_FILE", bomFile)
	t.Setenv("PUBLISHER_KEY", setup.opts.publisherKey)
	s := &shell{}
	out := &bytes.Buffer{}
	s.root().SetOut(out)
	t.Cleanup(s.close)
	if err := s.dispatch([]string{"show", "dns", "example.com"}); err != nil {
		t.Fatal(err)
	}
	if s.opts.ownersFile != bomFile || s.opts.publisherKey != setup.opts.publisherKey || !strings.Contains(out.String(), "public-key=") {
		t.Fatal("container ownership environment was not applied")
	}
	override := &shell{}
	override.root().SetOut(&bytes.Buffer{})
	t.Cleanup(override.close)
	if err := override.dispatch([]string{"--owners-file", setup.opts.ownersFile, "--publisher-key", "explicit-key", "show", "dns", "example.com"}); err != nil {
		t.Fatal(err)
	}
	if override.opts.ownersFile != setup.opts.ownersFile || override.opts.publisherKey != "explicit-key" {
		t.Fatal("explicit flags failed to override environment")
	}
}
