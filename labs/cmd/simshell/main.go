package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"d7024e/kademlia"
	"github.com/spf13/cobra"
)

const (
	defaultAddress   = "127.0.0.1:8000"
	defaultTransport = "simulated"
)

type options struct {
	transport     string
	addr          string
	listenAddr    string
	nodes         int
	bootstrapAddr string
	bootstrapID   string
}

type shell struct {
	opts    options
	node    *kademlia.Kademlia
	contact kademlia.Contact
	network kademlia.Node
	peers   []*kademlia.Kademlia
}

func main() {
	s := &shell{}
	cmd := s.root()
	err := cmd.Execute()
	s.close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (s *shell) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "simshell",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Name() == "help" || isCommandOrParent(cmd, "test") {
				return nil
			}
			return s.configure()
		},
		RunE: func(*cobra.Command, []string) error {
			if err := s.configure(); err != nil {
				return err
			}
			s.repl()
			return nil
		},
	}

	root.PersistentFlags().StringVar(&s.opts.transport, "transport", defaultTransport, "network transport: simulated or udp")
	root.PersistentFlags().StringVar(&s.opts.addr, "addr", envDefault("ADDR", defaultAddress), "advertised local node address")
	root.PersistentFlags().StringVar(&s.opts.listenAddr, "listen-addr", envDefault("LISTEN_ADDR", ""), "local bind address; defaults to --addr")
	root.PersistentFlags().IntVar(&s.opts.nodes, "nodes", 1, "number of in-process simulated nodes")
	root.PersistentFlags().StringVar(&s.opts.bootstrapAddr, "bootstrap-addr", envDefault("BOOTSTRAP_ADDR", ""), "bootstrap node address")
	root.PersistentFlags().StringVar(&s.opts.bootstrapID, "bootstrap-id", envDefault("BOOTSTRAP_ID", ""), "bootstrap node ID")

	root.AddCommand(
		&cobra.Command{Use: "serve", Args: cobra.NoArgs, Run: func(*cobra.Command, []string) {
			fmt.Printf("serving %s node %s id=%s\n", s.opts.transport, s.contact.Address, shortID(s.contact.ID))
			select {}
		}},
		&cobra.Command{Use: "ping IP:PORT", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error {
			return s.ping(a[0])
		}},
		&cobra.Command{Use: "put FILENAME", Aliases: []string{"store"}, Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error {
			return s.put(a[0])
		}},
		&cobra.Command{Use: "get KEY [FILENAME]", Args: cobra.RangeArgs(1, 2), RunE: func(_ *cobra.Command, a []string) error {
			out := ""
			if len(a) == 2 {
				out = a[1]
			}
			return s.get(a[0], out)
		}},
		s.testCmd(),
		s.showCmd(),
		&cobra.Command{Use: "exit", Aliases: []string{"quit"}, Args: cobra.NoArgs, Run: func(*cobra.Command, []string) {
			s.close()
			os.Exit(0)
		}},
	)
	return root
}

func isCommandOrParent(cmd *cobra.Command, name string) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Name() == name {
			return true
		}
	}
	return false
}

func (s *shell) configure() error {
	if s.node != nil {
		return nil
	}
	if s.opts.transport == "" {
		s.opts.transport = defaultTransport
	}
	if s.opts.addr == "" {
		s.opts.addr = defaultAddress
	}
	if s.opts.nodes <= 0 {
		return fmt.Errorf("--nodes must be at least 1")
	}

	switch strings.ToLower(s.opts.transport) {
	case "simulated", "sim":
		return s.configureSimulated()
	case "udp":
		return s.configureUDP()
	default:
		return fmt.Errorf("--transport must be simulated or udp")
	}
}

func (s *shell) configureSimulated() error {
	network := kademlia.NewSimulatedNetwork()
	nodes := make([]*kademlia.Kademlia, 0, s.opts.nodes)

	for i := 0; i < s.opts.nodes; i++ {
		addr, err := offsetAddress(s.opts.addr, i)
		if err != nil {
			return err
		}
		node, contact, transport, err := newNode(addr, addr, kademlia.NewSimulatedNodeWithNetwork(network))
		if err != nil {
			return err
		}
		go node.ListenForRPC()

		if i == 0 {
			s.node = node
			s.contact = contact
			s.network = transport
		} else if err := node.Join(nodes[0].RoutingTable.Me()); err != nil {
			return fmt.Errorf("join simulated node %s: %w", addr, err)
		}

		nodes = append(nodes, node)
	}

	s.peers = nodes
	return nil
}

func (s *shell) configureUDP() error {
	listenAddr := s.opts.listenAddr
	if listenAddr == "" {
		listenAddr = s.opts.addr
	}
	node, contact, transport, err := newNode(listenAddr, s.opts.addr, kademlia.NewUDPNode())
	if err != nil {
		return err
	}

	s.node = node
	s.contact = contact
	s.network = transport
	go node.ListenForRPC()

	if s.opts.bootstrapAddr == "" {
		return nil
	}

	bootstrapID := s.opts.bootstrapID
	if bootstrapID == "" {
		bootstrapID = hashID(s.opts.bootstrapAddr).String()
	}
	id := kademlia.NewKademliaID(bootstrapID)
	if id == nil {
		return fmt.Errorf("invalid bootstrap ID")
	}
	return node.Join(kademlia.NewContact(id, s.opts.bootstrapAddr))
}

func newNode(listenAddr string, advertiseAddr string, network kademlia.Node) (*kademlia.Kademlia, kademlia.Contact, kademlia.Node, error) {
	if _, err := parseAddress(listenAddr); err != nil {
		return nil, kademlia.Contact{}, nil, fmt.Errorf("invalid listen address: %w", err)
	}
	if _, err := parseAddress(advertiseAddr); err != nil {
		return nil, kademlia.Contact{}, nil, fmt.Errorf("invalid advertised address: %w", err)
	}
	if err := network.Listen(listenAddr); err != nil {
		return nil, kademlia.Contact{}, nil, err
	}

	contact := kademlia.NewContact(hashID(advertiseAddr), advertiseAddr)
	node := &kademlia.Kademlia{
		RoutingTable: kademlia.NewRoutingTable(contact),
		Network:      network,
		Alpha:        3,
		K:            10,
		RPCTimeout:   2 * time.Second,
	}
	return node, contact, network, nil
}

func (s *shell) repl() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("simshell %s node %s id=%s nodes=%d\n", s.opts.transport, s.contact.Address, shortID(s.contact.ID), len(s.peers))
	for {
		fmt.Print("simnet> ")
		if !scanner.Scan() {
			fmt.Println()
			s.close()
			return
		}
		args := strings.Fields(scanner.Text())
		if len(args) == 0 {
			continue
		}
		cmd := s.root()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			fmt.Println(err)
		}
	}
}

func (s *shell) showCmd() *cobra.Command {
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs}
	show.AddCommand(
		&cobra.Command{Use: "rt", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return s.show("rt")
		}},
		&cobra.Command{Use: "ds", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return s.show("ds")
		}},
		&cobra.Command{Use: "nodes", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return s.show("nodes")
		}},
	)
	return show
}

func (s *shell) testCmd() *cobra.Command {
	var nodes int
	var addr string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Demonstrate the CLI commands required by the lab spec",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runCLIDemoTest(nodes, addr)
		},
	}
	cmd.Flags().IntVar(&nodes, "nodes", 3, "number of in-process simulated nodes for the CLI demo")
	cmd.Flags().StringVar(&addr, "addr", defaultAddress, "first simulated node address")

	return cmd
}

func runCLIDemoTest(nodes int, addr string) error {
	if nodes < 2 {
		return fmt.Errorf("--nodes must be at least 2 for the CLI demo")
	}

	testShell := &shell{
		opts: options{
			transport: defaultTransport,
			addr:      addr,
			nodes:     nodes,
		},
	}
	defer testShell.close()

	if err := testShell.configure(); err != nil {
		return err
	}

	fmt.Printf("demo: started %d simulated nodes\n", len(testShell.peers))

	targetAddress, err := offsetAddress(addr, nodes-1)
	if err != nil {
		return err
	}
	fmt.Printf("demo: ping %s\n", targetAddress)
	if err = testShell.ping(targetAddress); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	payload := []byte("kademlia CLI demo " + time.Now().UTC().Format(time.RFC3339Nano))
	inputFile, err := os.CreateTemp("", "simshell-put-*.txt")
	if err != nil {
		return err
	}
	inputName := inputFile.Name()
	defer os.Remove(inputName)

	if _, err = inputFile.Write(payload); err != nil {
		inputFile.Close()
		return err
	}
	if err = inputFile.Close(); err != nil {
		return err
	}

	key := hashHex(payload)
	fmt.Printf("demo: put %s\n", inputName)
	if err = testShell.put(inputName); err != nil {
		return fmt.Errorf("put failed: %w", err)
	}
	fmt.Printf("demo: expected key %s\n", shortKey(key))

	outputFile, err := os.CreateTemp("", "simshell-get-*.txt")
	if err != nil {
		return err
	}
	outputName := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputName)

	fmt.Printf("demo: get %s %s\n", shortKey(key), outputName)
	if err = testShell.get(key, outputName); err != nil {
		return fmt.Errorf("get failed: %w", err)
	}
	found, err := os.ReadFile(outputName)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, found) {
		return fmt.Errorf("get wrote different data")
	}

	fmt.Println("demo: show rt")
	if err = testShell.show("rt"); err != nil {
		return fmt.Errorf("show rt failed: %w", err)
	}

	fmt.Println("demo: show ds")
	if err = testShell.show("ds"); err != nil {
		return fmt.Errorf("show ds failed: %w", err)
	}

	fmt.Println("demo: exit command is registered")
	fmt.Println("demo: PASS")
	return nil
}

func (s *shell) ping(rawAddr string) error {
	addr, err := parseAddress(rawAddr)
	if err != nil {
		return err
	}

	start := time.Now()
	target := kademlia.NewContact(hashID(addr), addr)
	contacts := s.node.LookupContact(&target)

	if addr == s.contact.Address {
		fmt.Printf("pong from %s in %s\n", addr, time.Since(start))
		return nil
	}
	for _, contact := range contacts {
		if contact.Address == addr {
			fmt.Printf("pong from %s in %s\n", addr, time.Since(start))
			return nil
		}
	}
	return fmt.Errorf("no node reached at %s", addr)
}

func (s *shell) put(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	key, err := s.node.Store(data)
	if err != nil {
		return err
	}
	fmt.Println(key)
	return nil
}

func (s *shell) get(key string, filename string) error {
	key = strings.ToLower(key)
	if !validKey(key) {
		return fmt.Errorf("key must be a %d-character hex SHA-256 hash", kademlia.IDLength*2)
	}
	data, err := s.node.LookupData(key)
	if err != nil {
		return err
	}
	if hashHex(data) != key {
		return fmt.Errorf("stored value failed hash verification")
	}
	if filename != "" {
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return err
		}
		fmt.Printf("value saved to %s\n", filename)
		return nil
	}
	fmt.Printf("value: %s\n", string(data))
	return nil
}

func (s *shell) show(what string) error {
	switch what {
	case "rt":
		for _, index := range s.node.RoutingTable.NonEmptyBucketIndices() {
			contacts := s.node.RoutingTable.ContactsInBucket(index)
			fmt.Printf("bucket %d: %d contacts\n", index, len(contacts))
			for _, contact := range contacts {
				fmt.Printf("  %s %s\n", shortID(contact.ID), contact.Address)
			}
		}
		if len(s.node.RoutingTable.NonEmptyBucketIndices()) == 0 {
			fmt.Println("routing table is empty")
		}
	case "ds":
		if len(s.node.DataStore) == 0 {
			fmt.Println("data store is empty")
			return nil
		}
		for key, data := range s.node.DataStore {
			fmt.Printf("%s bytes=%d\n", shortKey(key), len(data))
		}
	case "nodes":
		if len(s.peers) == 0 {
			fmt.Printf("%s %s\n", shortID(s.contact.ID), s.contact.Address)
			return nil
		}
		for _, node := range s.peers {
			contact := node.RoutingTable.Me()
			fmt.Printf("%s %s\n", shortID(contact.ID), contact.Address)
		}
	default:
		return fmt.Errorf("usage: show rt|ds|nodes")
	}
	return nil
}

func (s *shell) close() {
	if s.network != nil {
		_ = s.network.Close()
	}
	for i, node := range s.peers {
		if i == 0 || node.Network == nil {
			continue
		}
		_ = node.Network.Close()
	}
}

func hashID(address string) *kademlia.KademliaID {
	material, err := nodeIDMaterial(address)
	if err != nil {
		material = address
	}
	return kademlia.NewKademliaID(hashHex([]byte(material)))
}

func envDefault(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func nodeIDMaterial(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return "", fmt.Errorf("expected IP:PORT")
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", fmt.Errorf("expected numeric port")
	}
	return host + "|" + port, nil
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validKey(key string) bool {
	if len(key) != kademlia.IDLength*2 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func parseAddress(raw string) (string, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" {
		return "", fmt.Errorf("expected IP:PORT")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	return raw, nil
}

func offsetAddress(raw string, offset int) (string, error) {
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("expected IP:PORT")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port+offset > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(port+offset)), nil
}

func shortID(id *kademlia.KademliaID) string {
	if id == nil {
		return "<nil>"
	}
	return shortKey(id.String())
}

func shortKey(key string) string {
	return key[:4] + "..." + key[len(key)-4:]
}
