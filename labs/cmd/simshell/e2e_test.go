package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"d7024e/kademlia"
	"d7024e/metadata"
)

type cliWire struct {
	mu              sync.Mutex
	blocked         string
	corruptBlob     string
	tamperedReplies int
	sent            map[string]int
}

type cliTransport struct {
	kademlia.Node
	address string
	wire    *cliWire
}

func (n *cliTransport) SendData(address string, data []byte) error {
	var message struct {
		Type, Key string
		Found     bool
	}
	if err := json.Unmarshal(data, &message); err != nil {
		return err
	}
	n.wire.mu.Lock()
	blocked := n.wire.blocked != "" && (n.address == n.wire.blocked || address == n.wire.blocked)
	tamper := message.Type == "FIND_VALUE_REPLY" && message.Found && message.Key == n.wire.corruptBlob
	if tamper {
		n.wire.tamperedReplies++
	}
	n.wire.mu.Unlock()
	if blocked {
		return fmt.Errorf("test partition")
	}
	if tamper {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		value, err := json.Marshal([]byte("tampered remote package"))
		if err != nil {
			return err
		}
		fields["value"] = value
		data, err = json.Marshal(fields)
		if err != nil {
			return err
		}
	}
	if err := n.Node.SendData(address, data); err != nil {
		return err
	}
	n.wire.mu.Lock()
	n.wire.sent[address+"|"+message.Type]++
	n.wire.mu.Unlock()
	return nil
}

func (w *cliWire) count(address, kind string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent[address+"|"+kind]
}

func observedCLINetwork(t *testing.T) (*shell, *bytes.Buffer, string, *cliWire) {
	t.Helper()
	s, out, dir := registryShell(t, 1)
	s.close()
	// Build fresh independent nodes, wrapping transports before starting RPCs.
	wire := &cliWire{sent: map[string]int{}}
	network := kademlia.NewSimulatedNetwork()
	nodes := make([]*kademlia.Kademlia, 3)
	for i := range nodes {
		address := fmt.Sprintf("127.0.0.1:%d", 18900+i)
		transport := &cliTransport{kademlia.NewSimulatedNodeWithNetwork(network), address, wire}
		node, contact, _, err := newNode(address, address, transport)
		if err != nil {
			t.Fatal(err)
		}
		node.K, node.RPCRetries, node.RPCTimeout = 3, 1, time.Second
		node.OwnershipVerifier = s.verifier
		nodes[i] = node
		done := make(chan struct{})
		go func() { node.ListenForRPC(); close(done) }()
		t.Cleanup(func() { _ = transport.Close(); <-done })
		if i == 0 {
			s.node, s.network, s.contact = node, transport, contact
		}
	}
	for _, node := range nodes {
		for _, peer := range nodes {
			if node != peer {
				node.RoutingTable.AddContact(peer.RoutingTable.Me())
			}
		}
	}
	s.peers = nodes
	return s, out, dir, wire
}

func cliHeads(t *testing.T, nodes []*kademlia.Kademlia) [][]byte {
	t.Helper()
	key := kademlia.LatestKey("example.com", "demo")
	heads := make([][]byte, len(nodes))
	for i, node := range nodes {
		heads[i] = node.DataSnapshot()[key]
	}
	return heads
}

func TestCLIForcedUpdatesReachReceiversAndPreserveHeads(t *testing.T) {
	s, out, dir, wire := observedCLINetwork(t)
	file := filepath.Join(dir, "package")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "example.com:demo:1", file)
	if err := os.WriteFile(file, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "--prev=1", "example.com:demo:2", file)
	replayHeads := cliHeads(t, s.peers)
	runShell(t, s, out, "publish", "--force", "--prev=1", "example.com:demo:2", file)
	if !reflect.DeepEqual(replayHeads, cliHeads(t, s.peers)) {
		t.Fatal("duplicate CLI replay changed heads")
	}
	if err := os.WriteFile(file, []byte("different signed replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"publish", "--force", "--prev=0", "example.com:demo:3", file},
		{"publish", "--prev=1", "--force", "example.com:demo:3", file},
		{"publish", "--force", "example.com:demo:1", file},
		{"publish", "--force", "--prev=1", "example.com:demo:2", file},
		{"publish", "--force", "--prev=" + strings.Repeat("a", 64), "example.com:demo:3", file},
	} {
		before := cliHeads(t, s.peers)
		counts := make([]int, len(s.peers))
		for i, node := range s.peers {
			counts[i] = wire.count(node.RoutingTable.Me().Address, "UPDATE_LATEST")
		}
		out.Reset()
		if err := s.dispatch(args); err == nil {
			t.Fatalf("invalid update accepted: %v", args)
		}
		if !reflect.DeepEqual(before, cliHeads(t, s.peers)) {
			t.Fatalf("receiver head changed: %v", args)
		}
		for i, node := range s.peers {
			if i > 0 && wire.count(node.RoutingTable.Me().Address, "UPDATE_LATEST") <= counts[i] {
				t.Fatalf("CLI rejection never reached receiver %d: %v", i, args)
			}
		}
	}
	// Deliberately sign with a non-owner key; --force must reach the receivers.
	_, outsider, err := metadata.GeneratePublisherKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := metadata.EncodePublisherPrivateKey(outsider)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.opts.publisherKey, []byte(encoded), 0600); err != nil {
		t.Fatal(err)
	}
	before := cliHeads(t, s.peers)
	count := wire.count(s.peers[1].RoutingTable.Me().Address, "UPDATE_LATEST")
	if err := s.dispatch([]string{"publish", "--force", "example.com:demo:3", file}); err == nil {
		t.Fatal("non-owner accepted")
	}
	if !reflect.DeepEqual(before, cliHeads(t, s.peers)) || wire.count(s.peers[1].RoutingTable.Me().Address, "UPDATE_LATEST") <= count {
		t.Fatal("non-owner rejection did not preserve remote state or cross network")
	}
}

func TestCLIInstallCatchesUpSeveralMissedPublications(t *testing.T) {
	s, out, dir, wire := observedCLINetwork(t)
	file := filepath.Join(dir, "package")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	runShell(t, s, out, "publish", "example.com:demo:1", file)
	stale := s.peers[2]
	old := stale.DataSnapshot()
	wire.mu.Lock()
	wire.blocked = stale.RoutingTable.Me().Address
	wire.mu.Unlock()
	for _, version := range []int{4, 8, 12} {
		if err := os.WriteFile(file, []byte(fmt.Sprintf("release %d", version)), 0600); err != nil {
			t.Fatal(err)
		}
		runShell(t, s, out, "publish", fmt.Sprintf("example.com:demo:%d", version), file)
	}
	if !reflect.DeepEqual(old, stale.DataSnapshot()) {
		t.Fatal("offline CLI replica did not miss publications")
	}
	wire.mu.Lock()
	wire.blocked = ""
	wire.mu.Unlock()
	reader := &shell{node: stale, peers: s.peers, contact: stale.RoutingTable.Me()}
	reader.root().SetOut(out)
	filename := filepath.Join(dir, "latest")
	// Actual replica replies carry tampered bytes for the signed blob reference.
	// Verification must fail before touching an existing installation file.
	var pointer metadata.LatestPointer
	if err := json.Unmarshal(cliHeads(t, s.peers)[0], &pointer); err != nil {
		t.Fatal(err)
	}
	var record metadata.VersionRecord
	if err := json.Unmarshal(s.node.DataSnapshot()[pointer.VersionRecordHash], &record); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	wire.corruptBlob = record.BlobHash
	wire.mu.Unlock()
	if err := os.WriteFile(filename, []byte("keep existing install"), 0600); err != nil {
		t.Fatal(err)
	}
	var rejected *kademlia.LatestError
	if err := reader.dispatch([]string{"install", "example.com:demo:latest", "--output", filename}); !errors.As(err, &rejected) || rejected.Status != "authentication" {
		t.Fatalf("tampered remote blob did not fail authentication: %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != "keep existing install" {
		t.Fatal("failed authenticity check overwrote installation")
	}
	wire.mu.Lock()
	tampered := wire.tamperedReplies
	wire.corruptBlob = ""
	wire.mu.Unlock()
	if tampered == 0 {
		t.Fatal("test did not corrupt any real remote response")
	}
	got := runShell(t, reader, out, "install", "example.com:demo:latest", "--output", filename)
	data, err = os.ReadFile(filename)
	if err != nil || string(data) != "release 12" || strings.Contains(got, "received from "+stale.RoutingTable.Me().Address) {
		t.Fatalf("remote latest install: %q %s %v", data, got, err)
	}
	if !bytes.Equal(cliHeads(t, s.peers)[0], cliHeads(t, s.peers)[2]) {
		t.Fatal("install did not commit caught-up receiver head")
	}
	got = runShell(t, reader, out, "show", "example.com:demo")
	if strings.Count(got, "version=") != 4 {
		t.Fatalf("missing history: %s", got)
	}
	filename = filepath.Join(dir, "original")
	runShell(t, reader, out, "install", "example.com:demo:1", "--output", filename)
	data, err = os.ReadFile(filename)
	if err != nil || string(data) != "original" {
		t.Fatalf("historical install: %q %v", data, err)
	}
}

func freeUDPAddress(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestCLIUDPBootstrapPublicationAndRemoteInstall(t *testing.T) {
	setup, _, dir := registryShell(t, 1)
	setup.close()
	newUDP := func(bootstrap string) (*shell, *bytes.Buffer) {
		s := &shell{}
		out := &bytes.Buffer{}
		s.root().SetOut(out)
		t.Cleanup(s.close)
		args := []string{"--transport", "udp", "--addr", freeUDPAddress(t), "--owners-file", setup.opts.ownersFile, "--publisher-key", setup.opts.publisherKey, "--k", "3", "--alpha", "2", "--b", "2"}
		if bootstrap != "" {
			args = append(args, "--bootstrap-addr", bootstrap)
		}
		args = append(args, "show", "nodes")
		if err := s.dispatch(args); err != nil {
			t.Fatal(err)
		}
		return s, out
	}
	publisher, out := newUDP("")
	receiver, _ := newUDP(publisher.contact.Address)
	file := filepath.Join(dir, "package")
	for _, version := range []int{1, 2} {
		if err := os.WriteFile(file, []byte(fmt.Sprintf("udp release %d", version)), 0600); err != nil {
			t.Fatal(err)
		}
		runShell(t, publisher, out, "publish", fmt.Sprintf("example.com:demo:%d", version), file)
	}
	if !bytes.Equal(cliHeads(t, []*kademlia.Kademlia{publisher.node})[0], cliHeads(t, []*kademlia.Kademlia{receiver.node})[0]) {
		t.Fatal("UDP receiver did not commit publication")
	}
	reader, readOut := newUDP(publisher.contact.Address)
	if len(reader.node.DataSnapshot()) != 0 {
		t.Fatal("fresh UDP reader unexpectedly has cached records")
	}
	for _, version := range []string{"1", "latest"} {
		filename := filepath.Join(dir, "udp-"+version)
		got := runShell(t, reader, readOut, "install", "example.com:demo:"+version, "--output", filename)
		data, err := os.ReadFile(filename)
		want := "udp release 2"
		if version == "1" {
			want = "udp release 1"
		}
		if err != nil || string(data) != want || strings.Contains(got, "received from "+reader.contact.Address) {
			t.Fatalf("UDP remote install: %q %s %v", data, got, err)
		}
	}
	runShell(t, reader, readOut, "show", "dns", "example.com")
	runShell(t, reader, readOut, "show", "example.com:demo")
}

// Run the real main/REPL in a child so exit and stdin cannot terminate the suite.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("D7024E_CLI_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"simshell"}, os.Args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing CLI arguments")
}

func TestCLIInteractiveProcessRegression(t *testing.T) {
	setup, _, dir := registryShell(t, 1)
	setup.close()
	file, latest, historical := filepath.Join(dir, "package"), filepath.Join(dir, "latest"), filepath.Join(dir, "historical")
	if err := os.WriteFile(file, []byte("process package"), 0600); err != nil {
		t.Fatal(err)
	}
	script := strings.Join([]string{
		"", "ping 127.0.0.1:19001", "put " + file, "get " + hashHex([]byte("process package")),
		"publish example.com:demo:1 " + file, "publish --prev=1 example.com:demo:2 " + file,
		"publish --force --prev=0 example.com:demo:3 " + file,
		"show example.com:demo", "show dns example.com", "show ds", "show rt", "show nodes",
		"install example.com:demo:latest --output " + latest, "install example.com:demo:1 --output " + historical,
		"show unknown.com:missing", "quit", "",
	}, "\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIProcess$", "--", "--addr", "127.0.0.1:19000", "--nodes", "2", "--k", "2", "--owners-file", setup.opts.ownersFile, "--publisher-key", setup.opts.publisherKey)
	cmd.Env = append(os.Environ(), "D7024E_CLI_PROCESS=1")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive CLI: %v\n%s", err, output)
	}
	for _, want := range []string{"pong from", "published example.com:demo:2", "rejected:", "verified history", "public-key=", "latest-pointer", "version-record", "blob bytes=", "installed example.com:demo:2", "installed example.com:demo:1", "pointer not found"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("interactive output missing %q: %s", want, output)
		}
	}
	for _, filename := range []string{latest, historical} {
		data, err := os.ReadFile(filename)
		if err != nil || string(data) != "process package" {
			t.Fatalf("interactive install %q: %q %v", filename, data, err)
		}
	}
}

func TestCLIDemoAndInputFailures(t *testing.T) {
	if err := runCLIDemoTest(2, "127.0.0.1:19100"); err != nil {
		t.Fatal(err)
	}
	if err := runCLIDemoTest(1, "127.0.0.1:19100"); err == nil {
		t.Fatal("accepted one-node demo")
	}
	for _, args := range [][]string{{"--nodes=0", "show", "ds"}, {"--b=257", "show", "ds"}, {"--transport=invalid", "show", "ds"}, {"--addr=bad", "show", "ds"}, {"--transport=udp", "--bootstrap-id=bad", "--bootstrap-addr=127.0.0.1:19101", "--addr=" + freeUDPAddress(t), "show", "ds"}} {
		s := &shell{}
		s.root().SetOut(&bytes.Buffer{})
		t.Cleanup(s.close)
		if err := s.dispatch(args); err == nil {
			t.Fatalf("invalid configuration accepted: %v", args)
		}
	}
	s, out, dir := registryShell(t, 1)
	for _, args := range [][]string{{"ping", "bad"}, {"get", "bad"}, {"get", strings.Repeat("a", 64)}, {"put", filepath.Join(dir, "missing")}, {"publish", "bad", "missing"}, {"publish", "example.com:demo:latest", "missing"}, {"install", "example.com:demo:bad"}} {
		out.Reset()
		if err := s.dispatch(args); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
}
