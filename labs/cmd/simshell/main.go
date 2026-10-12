package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"d7024e/kademlia"
	"github.com/spf13/cobra"
)

const (
	defaultAddress   = "127.0.0.1:8000"
	defaultTransport = "simulated"
	defaultForce     = false // enable for testing
)

type options struct {
	alpha, k                 int
	branchBits               int
	replication, refresh     time.Duration
	transport                string
	addr                     string
	listenAddr               string
	nodes                    int
	bootstrapAddr            string
	bootstrapID              string
	force                    bool
	prev                     string
	publisherKey, ownersFile string
}

type shell struct {
	opts     options
	node     *kademlia.Kademlia
	contact  kademlia.Contact
	network  kademlia.Node
	peers    []*kademlia.Kademlia
	command  *cobra.Command
	verifier kademlia.OwnershipVerifier
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
	if s.command != nil {
		return s.command
	}
	root := &cobra.Command{
		Use:           "simshell",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Name() == "help" || cmd.Name() == "keygen" || isCommandOrParent(cmd, "test") {
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

	root.PersistentFlags().IntVar(&s.opts.alpha, "alpha", 3, "parallel lookup probes")
	root.PersistentFlags().IntVar(&s.opts.k, "k", 10, "bucket capacity and replica count")
	root.PersistentFlags().IntVar(&s.opts.branchBits, "b", 1, "routing tree acceleration parameter (1..256)")
	root.PersistentFlags().DurationVar(&s.opts.replication, "replication-period", time.Hour, "replica repair interval")
	root.PersistentFlags().DurationVar(&s.opts.refresh, "refresh-period", time.Hour, "stale bucket refresh interval")
	root.PersistentFlags().StringVar(&s.opts.transport, "transport", defaultTransport, "network transport: simulated or udp")
	root.PersistentFlags().StringVar(&s.opts.addr, "addr", envDefault("ADDR", defaultAddress), "advertised local node address")
	root.PersistentFlags().StringVar(&s.opts.listenAddr, "listen-addr", envDefault("LISTEN_ADDR", ""), "local bind address; defaults to --addr")
	root.PersistentFlags().IntVar(&s.opts.nodes, "nodes", 1, "number of in-process simulated nodes")
	root.PersistentFlags().StringVar(&s.opts.bootstrapAddr, "bootstrap-addr", envDefault("BOOTSTRAP_ADDR", ""), "bootstrap node address")
	root.PersistentFlags().StringVar(&s.opts.bootstrapID, "bootstrap-id", envDefault("BOOTSTRAP_ID", ""), "bootstrap node ID")
	root.PersistentFlags().StringVar(&s.opts.publisherKey, "publisher-key", "", "local hex Ed25519 private-key file for publish")
	root.PersistentFlags().StringVar(&s.opts.ownersFile, "owners-file", "", "fake DNS JSON map of domains to public-key hex; default: real DNS TXT")

	root.AddCommand(
		&cobra.Command{Use: "serve", Args: cobra.NoArgs, Run: func(*cobra.Command, []string) {
			fmt.Printf("serving %s node %s id=%s\n", s.opts.transport, s.contact.Address, shortID(s.contact.ID))
			select {}
		}},
		&cobra.Command{Use: "ping IP:PORT", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error {
			return s.ping(a[0])
		}},
		s.publishCmd(),
		s.installCmd(),
		keygenCmd(),
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
	s.command = root
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
	if s.opts.branchBits < 0 || s.opts.branchBits > 256 || s.opts.alpha < 0 || s.opts.k < 0 || s.opts.replication < 0 || s.opts.refresh < 0 {
		return fmt.Errorf("invalid Kademlia parameters")
	}
	verifier, err := loadOwners(s.opts.ownersFile)
	if err != nil {
		return err
	}
	s.verifier = verifier

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
		if s.opts.alpha > 0 {
			node.Alpha = s.opts.alpha
		}
		node.OwnershipVerifier = s.verifier
		if s.opts.k > 0 {
			node.K = s.opts.k
		}
		if s.opts.branchBits > 0 {
			node.RoutingTable = kademlia.NewRoutingTableWithBranchBits(contact, s.opts.branchBits)
		}
		node.ReplicationPeriod, node.RefreshPeriod = s.opts.replication, s.opts.refresh
		node.RoutingTable.SetCapacity(node.K)
		go node.ListenForRPC()

		if i == 0 {
			s.node = node
			s.contact = contact
			s.network = transport
		} else if err := node.Join(nodes[0].RoutingTable.Me()); err != nil {
			return fmt.Errorf("join simulated node %s: %w", addr, err)
		}

		node.StartMaintenance()
		nodes = append(nodes, node)
	}

	s.peers = nodes
	return nil
}

func (s *shell) configureUDP() error {
	var err error
	s.opts.addr, err = resolveUDPAddress(s.opts.addr)
	if err != nil {
		return err
	}
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
	node.OwnershipVerifier = s.verifier
	if s.opts.alpha > 0 {
		node.Alpha = s.opts.alpha
	}
	if s.opts.k > 0 {
		node.K = s.opts.k
	}
	if s.opts.branchBits > 0 {
		node.RoutingTable = kademlia.NewRoutingTableWithBranchBits(contact, s.opts.branchBits)
	}
	node.ReplicationPeriod, node.RefreshPeriod = s.opts.replication, s.opts.refresh
	node.RoutingTable.SetCapacity(node.K)
	go node.ListenForRPC()

	if s.opts.bootstrapAddr == "" {
		node.StartMaintenance()
		return nil
	}
	s.opts.bootstrapAddr, err = resolveUDPAddress(s.opts.bootstrapAddr)
	if err != nil {
		return err
	}

	bootstrapID := s.opts.bootstrapID
	if bootstrapID == "" {
		bootstrapID = hashID(s.opts.bootstrapAddr).String()
	}
	id := kademlia.NewKademliaID(bootstrapID)
	if id == nil {
		return fmt.Errorf("invalid bootstrap ID")
	}
	if err := node.Join(kademlia.NewContact(id, s.opts.bootstrapAddr)); err != nil {
		return err
	}
	node.StartMaintenance()
	return nil
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
		if err := s.dispatch(args); err != nil {
			fmt.Println(err)
		}
	}
}

// The interactive shell executes the same Cobra tree as one-shot commands.
func (s *shell) dispatch(args []string) error {
	cmd := s.root()
	defer func() {
		// Parsing/argument errors can happen before RunE resets command flags.
		// Preserve startup flags, but never carry publication or output flags
		// into the next interactive command after a failed invocation.
		for _, child := range cmd.Commands() {
			var names []string
			switch child.Name() {
			case "publish":
				names = []string{"force", "prev"}
			case "install":
				names = []string{"output"}
			}
			for _, name := range names {
				flag := child.Flags().Lookup(name)
				_ = flag.Value.Set(flag.DefValue)
				flag.Changed = false
			}
		}
	}()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func (s *shell) showCmd() *cobra.Command {
	show := &cobra.Command{Use: "show DOMAIN:PACKAGE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return s.showHistory(cmd.OutOrStdout(), a[0])
	}}
	show.AddCommand(
		&cobra.Command{Use: "rt", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return s.showTo(cmd.OutOrStdout(), "rt")
		}},
		&cobra.Command{Use: "ds", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return s.showTo(cmd.OutOrStdout(), "ds")
		}},
		&cobra.Command{Use: "nodes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return s.showTo(cmd.OutOrStdout(), "nodes")
		}},
		&cobra.Command{Use: "dns DOMAIN", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			return s.showDNS(cmd.OutOrStdout(), a[0])
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

	rtt, err := s.node.Ping(kademlia.Contact{Address: addr})
	if err != nil {
		return err
	}
	fmt.Printf("pong from %s in %s\n", addr, rtt)
	return nil
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
	data, source, err := s.node.LookupDataFrom(key)
	if err != nil {
		return err
	}
	if hashHex(data) != key {
		return fmt.Errorf("stored value failed hash verification")
	}
	fmt.Printf("received from %s id=%s\n", source.Address, shortID(source.ID))
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
	return s.showTo(os.Stdout, what)
}

func (s *shell) showTo(out io.Writer, what string) error {
	switch what {
	case "rt":
		buckets := s.node.RoutingTable.Snapshot()
		indices := make([]int, 0, len(buckets))
		for index := range buckets {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			contacts := buckets[index]
			fmt.Fprintf(out, "bucket %d: %d contacts\n", index, len(contacts))
			for _, contact := range contacts {
				fmt.Fprintf(out, "  %s %s\n", shortID(contact.ID), contact.Address)
			}
		}
		if len(buckets) == 0 {
			fmt.Fprintln(out, "routing table is empty")
		}
	case "ds":
		dataStore := s.node.DataSnapshot()
		if len(dataStore) == 0 {
			fmt.Fprintln(out, "data store is empty")
			return nil
		}
		keys := make([]string, 0, len(dataStore))
		for key := range dataStore {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			displayStoredValue(out, key, dataStore[key])
		}
	case "nodes":
		if len(s.peers) == 0 {
			fmt.Fprintf(out, "%s %s\n", shortID(s.contact.ID), s.contact.Address)
			return nil
		}
		for _, node := range s.peers {
			contact := node.RoutingTable.Me()
			fmt.Fprintf(out, "%s %s\n", shortID(contact.ID), contact.Address)
		}
	default:
		return fmt.Errorf("usage: show rt|ds|nodes")
	}
	return nil
}

func (s *shell) close() {
	if s.network != nil {
		_ = s.network.Close()
		if s.node != nil {
			s.node.StopMaintenance()
		}
	}
	for i, node := range s.peers {
		if i == 0 || node.Network == nil {
			continue
		}
		_ = node.Network.Close()
		node.StopMaintenance()
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

// Advertise concrete IP addresses, so all peers derive ID = SHA256(IP|port).
func resolveUDPAddress(raw string) (string, error) {
	if _, err := parseAddress(raw); err != nil {
		return "", err
	}
	address, err := net.ResolveUDPAddr("udp4", raw)
	if err != nil || address.IP == nil {
		return "", fmt.Errorf("resolve advertised address %s: %v", raw, err)
	}
	return net.JoinHostPort(address.IP.String(), strconv.Itoa(address.Port)), nil
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
	if len(key) <= 12 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}
