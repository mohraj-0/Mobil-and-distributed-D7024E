package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"d7024e/kademlia"
	/*dfdfddkgjdslvmldmvlds
	 */
	"github.com/spf13/cobra"
)

const commandTimeout = time.Second

type simNode struct {
	addr    kademlia.Address
	contact kademlia.Contact
	conn    kademlia.Connection

	mu      sync.RWMutex
	store   map[string]string
	replies chan kademlia.Message
}

type simShell struct {
	network *kademlia.SimulatedNetwork
	nodes   map[kademlia.Address]*simNode
	out     io.Writer
}

func main() {
	shell := newSimShell(os.Stdout)
	rootCmd := shell.newRootCommand()
	rootCmd.SetArgs(os.Args[1:])

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newSimShell(out io.Writer) *simShell {
	return &simShell{
		network: kademlia.NewSimulatedNetwork(),
		nodes:   make(map[kademlia.Address]*simNode),
		out:     out,
	}
}

func (s *simShell) newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "simshell",
		Short:         "Test a simulated Kademlia network",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.runREPL()
		},
	}
	rootCmd.SetOut(s.out)
	rootCmd.SetErr(s.out)

	rootCmd.AddCommand(
		s.newNodeCommand(),
		s.newPingCommand(),
		s.newStoreCommand(),
		s.newPutCommand(),
		s.newGetCommand(),
		s.newExitCommand(),
	)

	return rootCmd
}

func (s *simShell) runREPL() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Fprintln(s.out, "Simulated Kademlia shell. Type help for commands.")
	for {
		fmt.Fprint(s.out, "simnet> ")
		if !scanner.Scan() {
			fmt.Fprintln(s.out)
			s.closeAll()
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		args := strings.Fields(line)
		if args[0] == "quit" || args[0] == "exit" {
			s.closeAll()
			return nil
		}

		cmd := s.newRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			fmt.Fprintln(s.out, err)
		}
	}
}

func (s *simShell) newNodeCommand() *cobra.Command {
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "Manage simulated nodes",
	}

	nodeCmd.AddCommand(&cobra.Command{
		Use:   "add <ip:port> [id-hex]",
		Short: "Add a simulated node",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 2 {
				id = args[1]
			}
			return s.addNode(args[0], id)
		},
	})

	nodeCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List simulated nodes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s.listNodes()
			return nil
		},
	})

	nodeCmd.AddCommand(&cobra.Command{
		Use:   "close <ip:port>",
		Short: "Close a simulated node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.closeNode(args[0])
		},
	})

	return nodeCmd
}

func (s *simShell) newPingCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ping <from-ip:port> <to-ip:port>",
		Short: "Send PING and wait for PONG",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.ping(args[0], args[1])
		},
	}
}

func (s *simShell) newStoreCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "store <from-ip:port> <to-ip:port> <key> <value>",
		Short: "Store a value on a specific simulated node",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.storeValue(args[0], args[1], args[2], strings.Join(args[3:], " "))
		},
	}
}

func (s *simShell) newPutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "put <from-ip:port> <key> <value>",
		Short: "Store a value on the simulated node closest to the key",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.putValue(args[0], args[1], strings.Join(args[2:], " "))
		},
	}
}

func (s *simShell) newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <from-ip:port> <key>",
		Short: "Fetch a value from the simulated network",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.getValue(args[0], args[1])
		},
	}
}

func (s *simShell) newExitCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "exit",
		Aliases: []string{"quit"},
		Short:   "Exit the interactive shell",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			s.closeAll()
		},
	}
}

func (s *simShell) addNode(rawAddr string, rawID string) error {
	addr, err := parseAddress(rawAddr)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	if _, exists := s.nodes[addr]; exists {
		return fmt.Errorf("node already exists at %s", formatAddress(addr))
	}

	id, err := nodeID(rawAddr, rawID)
	if err != nil {
		return err
	}

	conn, err := s.network.Listen(addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	node := &simNode{
		addr:    addr,
		contact: kademlia.NewContact(id, formatAddress(addr)),
		conn:    conn,
		store:   make(map[string]string),
		replies: make(chan kademlia.Message, 1024),
	}
	s.nodes[addr] = node

	go s.serveNode(node)

	fmt.Fprintf(s.out, "added node %s id=%s\n", formatAddress(addr), id.String())
	return nil
}

func (s *simShell) ping(rawFrom string, rawTo string) error {
	from, to, err := s.requireRoute(rawFrom, rawTo)
	if err != nil {
		return err
	}

	if err := s.send(from.addr, to.addr, "PING"); err != nil {
		return err
	}

	reply, err := from.waitForReply("PONG", commandTimeout)
	if err != nil {
		return err
	}

	fmt.Fprintf(s.out, "pong from %s: %s\n", formatAddress(reply.From), string(reply.Payload))
	return nil
}

func (s *simShell) storeValue(rawFrom string, rawTo string, key string, value string) error {
	from, to, err := s.requireRoute(rawFrom, rawTo)
	if err != nil {
		return err
	}

	if err := s.send(from.addr, to.addr, fmt.Sprintf("STORE %s %s", key, value)); err != nil {
		return err
	}

	reply, err := from.waitForReply("STORED "+key, commandTimeout)
	if err != nil {
		return err
	}

	fmt.Fprintf(s.out, "stored %q on %s: %s\n", key, formatAddress(reply.From), string(reply.Payload))
	return nil
}

func (s *simShell) putValue(rawFrom string, key string, value string) error {
	from, err := s.requireNode(rawFrom)
	if err != nil {
		return err
	}

	target := keyID(key)
	closest := s.closestNode(target)
	if closest == nil {
		return fmt.Errorf("no nodes available")
	}

	if err := s.send(from.addr, closest.addr, fmt.Sprintf("STORE %s %s", key, value)); err != nil {
		return err
	}

	reply, err := from.waitForReply("STORED "+key, commandTimeout)
	if err != nil {
		return err
	}

	fmt.Fprintf(s.out, "put %q on closest node %s: %s\n", key, formatAddress(closest.addr), string(reply.Payload))
	return nil
}

func (s *simShell) getValue(rawFrom string, key string) error {
	from, err := s.requireNode(rawFrom)
	if err != nil {
		return err
	}

	for _, node := range s.closestNodes(keyID(key)) {
		if err := s.send(from.addr, node.addr, "GET "+key); err != nil {
			return err
		}

		reply, err := from.waitForReply("", commandTimeout)
		if err != nil {
			return err
		}

		payload := string(reply.Payload)
		if strings.HasPrefix(payload, "VALUE "+key+" ") {
			value := strings.TrimPrefix(payload, "VALUE "+key+" ")
			fmt.Fprintf(s.out, "value %q from %s: %s\n", key, formatAddress(reply.From), value)
			return nil
		}
	}

	return fmt.Errorf("key %q not found", key)
}

func (s *simShell) send(from kademlia.Address, to kademlia.Address, payload string) error {
	conn, err := s.network.Dial(to)
	if err != nil {
		return fmt.Errorf("dial %s failed: %w", formatAddress(to), err)
	}
	defer conn.Close()

	msg := kademlia.Message{
		From:    from,
		To:      to,
		Payload: []byte(payload),
	}
	if err := conn.Send(msg); err != nil {
		return fmt.Errorf("send failed: %w", err)
	}

	return nil
}

func (s *simShell) serveNode(node *simNode) {
	for {
		msg, err := node.conn.Recv()
		if err != nil {
			close(node.replies)
			return
		}

		payload := string(msg.Payload)
		switch {
		case payload == "PING":
			_ = s.send(node.addr, msg.From, "PONG")
		case strings.HasPrefix(payload, "STORE "):
			key, value, ok := strings.Cut(strings.TrimPrefix(payload, "STORE "), " ")
			if !ok {
				_ = s.send(node.addr, msg.From, "ERROR malformed STORE")
				continue
			}
			node.mu.Lock()
			node.store[key] = value
			node.mu.Unlock()
			_ = s.send(node.addr, msg.From, "STORED "+key)
		case strings.HasPrefix(payload, "GET "):
			key := strings.TrimSpace(strings.TrimPrefix(payload, "GET "))
			node.mu.RLock()
			value, ok := node.store[key]
			node.mu.RUnlock()
			if ok {
				_ = s.send(node.addr, msg.From, "VALUE "+key+" "+value)
			} else {
				_ = s.send(node.addr, msg.From, "NOT_FOUND "+key)
			}
		default:
			node.replies <- msg
		}
	}
}

func (n *simNode) waitForReply(prefix string, timeout time.Duration) (kademlia.Message, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case msg, ok := <-n.replies:
			if !ok {
				return kademlia.Message{}, fmt.Errorf("node %s is closed", formatAddress(n.addr))
			}
			if prefix == "" || strings.HasPrefix(string(msg.Payload), prefix) {
				return msg, nil
			}
		case <-timer.C:
			return kademlia.Message{}, fmt.Errorf("timed out waiting for reply on %s", formatAddress(n.addr))
		}
	}
}

func (s *simShell) listNodes() {
	if len(s.nodes) == 0 {
		fmt.Fprintln(s.out, "no nodes")
		return
	}

	addresses := make([]string, 0, len(s.nodes))
	for addr := range s.nodes {
		addresses = append(addresses, formatAddress(addr))
	}
	sort.Strings(addresses)

	for _, rawAddr := range addresses {
		addr, _ := parseAddress(rawAddr)
		node := s.nodes[addr]
		node.mu.RLock()
		storeCount := len(node.store)
		node.mu.RUnlock()
		fmt.Fprintf(s.out, "%s id=%s keys=%d\n", rawAddr, node.contact.ID.String(), storeCount)
	}
}

func (s *simShell) closeNode(rawAddr string) error {
	node, err := s.requireNode(rawAddr)
	if err != nil {
		return err
	}

	if err := node.conn.Close(); err != nil {
		return fmt.Errorf("close failed: %w", err)
	}
	delete(s.nodes, node.addr)
	fmt.Fprintln(s.out, "closed", formatAddress(node.addr))
	return nil
}

func (s *simShell) closeAll() {
	for addr, node := range s.nodes {
		_ = node.conn.Close()
		delete(s.nodes, addr)
	}
}

func (s *simShell) requireRoute(rawFrom string, rawTo string) (*simNode, *simNode, error) {
	from, err := s.requireNode(rawFrom)
	if err != nil {
		return nil, nil, err
	}
	to, err := s.requireNode(rawTo)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

func (s *simShell) requireNode(rawAddr string) (*simNode, error) {
	addr, err := parseAddress(rawAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	node := s.nodes[addr]
	if node == nil {
		return nil, fmt.Errorf("no node at %s", formatAddress(addr))
	}

	return node, nil
}

func (s *simShell) closestNode(target *kademlia.KademliaID) *simNode {
	nodes := s.closestNodes(target)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func (s *simShell) closestNodes(target *kademlia.KademliaID) []*simNode {
	nodes := make([]*simNode, 0, len(s.nodes))
	for _, node := range s.nodes {
		node.contact.CalcDistance(target)
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].contact.Less(&nodes[j].contact)
	})

	return nodes
}

func nodeID(rawAddr string, rawID string) (*kademlia.KademliaID, error) {
	if rawID == "" {
		sum := sha256.Sum256([]byte(rawAddr))
		return kademlia.NewKademliaID(fmt.Sprintf("%x", sum[:])), nil
	}
	if len(rawID) != kademlia.IDLength*2 {
		return nil, fmt.Errorf("id must be %d hex characters", kademlia.IDLength*2)
	}
	if _, err := hex.DecodeString(rawID); err != nil {
		return nil, fmt.Errorf("id must be valid hex: %w", err)
	}
	return kademlia.NewKademliaID(rawID), nil
}

func keyID(key string) *kademlia.KademliaID {
	sum := sha256.Sum256([]byte(key))
	return kademlia.NewKademliaID(fmt.Sprintf("%x", sum[:]))
}

func parseAddress(raw string) (kademlia.Address, error) {
	colon := strings.LastIndex(raw, ":")
	if colon == -1 {
		return kademlia.Address{}, fmt.Errorf("expected ip:port")
	}

	port, err := strconv.Atoi(raw[colon+1:])
	if err != nil {
		return kademlia.Address{}, err
	}
	if port < 1 || port > 65535 {
		return kademlia.Address{}, fmt.Errorf("port must be between 1 and 65535")
	}

	ip := raw[:colon]
	if ip == "" {
		return kademlia.Address{}, fmt.Errorf("ip cannot be empty")
	}

	return kademlia.Address{IP: ip, Port: port}, nil
}

func formatAddress(addr kademlia.Address) string {
	return fmt.Sprintf("%s:%d", addr.IP, addr.Port)
}
