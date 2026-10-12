package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"d7024e/metadata"
)

// Opt in with KADLAB_DOCKER_IMAGE=kadlab:part12-test. The test creates its own
// bridge network and containers, never redeploying the user's live Swarm stack.
func TestDockerUnifiedCLI(t *testing.T) {
	image := os.Getenv("KADLAB_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set KADLAB_DOCKER_IMAGE to run the isolated Docker UDP test")
	}
	name := fmt.Sprintf("kadlab-test-%d", time.Now().UnixNano())
	bootstrap, replica := name+"-bootstrap", name+"-replica"
	containers := []string{bootstrap, replica, name + "-publisher", name + "-reader"}
	run := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		if input != "" {
			cmd.Stdin = strings.NewReader(input)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	run("", "network", "create", name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", append([]string{"rm", "-f"}, containers...)...).Run()
		if output, err := exec.CommandContext(ctx, "docker", "network", "rm", name).CombinedOutput(); err != nil {
			t.Errorf("cleanup network: %v %s", err, output)
		}
	})
	dir := t.TempDir()
	pk, sk, err := metadata.GeneratePublisherKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := metadata.EncodePublisherPrivateKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, ownersFile := filepath.Join(dir, "publisher.key"), filepath.Join(dir, "owners.json")
	if err := os.WriteFile(keyFile, []byte(encoded), 0600); err != nil {
		t.Fatal(err)
	}
	owners, err := json.Marshal(map[string]string{"example.com": hex.EncodeToString(pk)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownersFile, owners, 0600); err != nil {
		t.Fatal(err)
	}
	publicMount := "type=bind,source=" + ownersFile + ",target=/etc/kadlab/owners.json,readonly"
	for _, server := range []string{bootstrap, replica} {
		args := []string{"run", "-d", "--name", server, "--network", name, "--mount", publicMount, "--env", "OWNERS_FILE=/etc/kadlab/owners.json", image, "/app/kadlab", "--transport", "udp", "--addr", server + ":8000", "--listen-addr", "0.0.0.0:8000", "--k", "3"}
		if server != bootstrap {
			args = append(args, "--bootstrap-addr", bootstrap+":8000")
		}
		args = append(args, "serve")
		run("", args...)
	}
	// Exercise the image launcher and inherited ownership defaults, with the
	// private key mounted only on the publisher client, never on replica servers.
	clientArgs := func(client string) []string {
		return []string{"run", "--rm", "-i", "--name", client, "--network", name, "--mount", publicMount, "--env", "OWNERS_FILE=/etc/kadlab/owners.json", "--env", "BOOTSTRAP_ADDR=" + bootstrap + ":8000"}
	}
	args := clientArgs(containers[2])
	args = append(args, "--mount", "type=bind,source="+keyFile+",target=/tmp/publisher.key,readonly", image, "sh", "-c", "printf first-release > /tmp/first; printf second-release > /tmp/second; /usr/local/bin/kadlab-shell --k 3 --publisher-key /tmp/publisher.key")
	output := run(strings.Join([]string{
		"ping " + replica + ":8000", "put /tmp/first", "show dns example.com",
		"publish example.com:demo:1 /tmp/first", "publish --prev=1 example.com:demo:2 /tmp/second",
		"publish --force --prev=0 example.com:demo:3 /tmp/second", "show example.com:demo", "show rt", "show ds", "exit", "",
	}, "\n"), args...)
	for _, want := range []string{"pong from", "published example.com:demo:2", "rejected:", "verified history", "version=2", "version=1", "public-key="} {
		if !strings.Contains(output, want) {
			t.Fatalf("Docker publisher missing %q: %s", want, output)
		}
	}
	if strings.Count(output, " accepted") < 6 {
		t.Fatalf("two publications did not reach three replicas: %s", output)
	}
	args = clientArgs(containers[3])
	args = append(args, image, "sh", "-c", "/usr/local/bin/kadlab-shell --k 3; printf '\nHISTORICAL='; cat /tmp/historical; printf '\nLATEST='; cat /tmp/latest")
	output = run("install example.com:demo:1 --output /tmp/historical\ninstall example.com:demo:latest --output /tmp/latest\nshow example.com:demo\nexit\n", args...)
	if !strings.Contains(output, "received from") || !bytes.Contains([]byte(output), []byte("HISTORICAL=first-release")) || !strings.Contains(output, "LATEST=second-release") {
		t.Fatalf("Docker remote installs failed: %s", output)
	}
}
