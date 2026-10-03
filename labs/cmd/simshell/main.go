package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	nodes         int
	httpPort      string
	bootstrapAddr string
	bootstrapID   string
}

type apiHandler struct {
	shell *shell
}

type nodeInfo struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type routingBucketInfo struct {
	Index    int        `json:"index"`
	Contacts []nodeInfo `json:"contacts"`
}

type dataStoreInfo struct {
	Key   string `json:"key"`
	Bytes int    `json:"bytes"`
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
			if cmd.Name() == "help" {
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
	root.PersistentFlags().StringVar(&s.opts.addr, "addr", defaultAddress, "local node address")
	root.PersistentFlags().IntVar(&s.opts.nodes, "nodes", 1, "number of in-process simulated nodes")
	root.PersistentFlags().StringVar(&s.opts.httpPort, "http", "8080", "HTTP API port")
	root.PersistentFlags().StringVar(&s.opts.bootstrapAddr, "bootstrap-addr", "", "bootstrap node address")
	root.PersistentFlags().StringVar(&s.opts.bootstrapID, "bootstrap-id", "", "bootstrap node ID")

	root.AddCommand(
		&cobra.Command{Use: "ping IP:PORT", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error {
			return s.ping(a[0])
		}},
		&cobra.Command{Use: "put FILENAME", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error {
			return s.put(a[0])
		}},
		&cobra.Command{Use: "get KEY [FILENAME]", Args: cobra.RangeArgs(1, 2), RunE: func(_ *cobra.Command, a []string) error {
			out := ""
			if len(a) == 2 {
				out = a[1]
			}
			return s.get(a[0], out)
		}},
		&cobra.Command{Use: "serve", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			if err := s.configure(); err != nil {
				return err
			}
			return s.serveHTTP()
		}},
		s.showCmd(),
		&cobra.Command{Use: "exit", Aliases: []string{"quit"}, Args: cobra.NoArgs, Run: func(*cobra.Command, []string) {
			s.close()
			os.Exit(0)
		}},
	)
	return root
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
		node, contact, transport, err := newNode(addr, kademlia.NewSimulatedNodeWithNetwork(network))
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
	node, contact, transport, err := newNode(s.opts.addr, kademlia.NewUDPNode())
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
		bootstrapID = hashHex([]byte(s.opts.bootstrapAddr))
	}
	id := kademlia.NewKademliaID(bootstrapID)
	if id == nil {
		return fmt.Errorf("invalid bootstrap ID")
	}
	return node.Join(kademlia.NewContact(id, s.opts.bootstrapAddr))
}

func newNode(addr string, network kademlia.Node) (*kademlia.Kademlia, kademlia.Contact, kademlia.Node, error) {
	if _, err := parseAddress(addr); err != nil {
		return nil, kademlia.Contact{}, nil, err
	}
	if err := network.Listen(addr); err != nil {
		return nil, kademlia.Contact{}, nil, err
	}

	contact := kademlia.NewContact(hashID(addr), addr)
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

func (s *shell) serveHTTP() error {
	mux := http.NewServeMux()
	handler := &apiHandler{shell: s}
	mux.Handle("/objects", handler)
	mux.Handle("/objects/", handler)
	mux.HandleFunc("/nodes", handler.nodes)
	mux.HandleFunc("/routing-table", handler.routingTable)
	mux.HandleFunc("/data-store", handler.dataStore)

	address := ":" + s.opts.httpPort
	fmt.Printf("simshell API listening on http://127.0.0.1%s\n", address)
	fmt.Printf("transport=%s node=%s id=%s nodes=%d\n", s.opts.transport, s.contact.Address, shortID(s.contact.ID), len(s.peers))
	return http.ListenAndServe(address, mux)
}

func (h *apiHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodPost:
		if request.URL.Path != "/objects" {
			http.NotFound(writer, request)
			return
		}

		data, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if len(data) == 0 {
			http.Error(writer, "empty request body", http.StatusBadRequest)
			return
		}

		hash, err := h.shell.node.Store(data)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}

		writer.Header().Set("Location", "/objects/"+hash)
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(hash + "\n"))

	case http.MethodGet:
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "objects" {
			http.NotFound(writer, request)
			return
		}

		key := strings.ToLower(parts[1])
		if !validKey(key) {
			http.Error(writer, "invalid object key", http.StatusBadRequest)
			return
		}

		data, err := h.shell.node.LookupData(key)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusNotFound)
			return
		}

		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(data)

	default:
		writer.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *apiHandler) nodes(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	nodes := h.shell.nodeInfos()
	writeJSON(writer, nodes)
}

func (h *apiHandler) routingTable(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	buckets := make([]routingBucketInfo, 0)
	for _, index := range h.shell.node.RoutingTable.NonEmptyBucketIndices() {
		contacts := h.shell.node.RoutingTable.ContactsInBucket(index)
		info := routingBucketInfo{
			Index:    index,
			Contacts: make([]nodeInfo, 0, len(contacts)),
		}
		for _, contact := range contacts {
			info.Contacts = append(info.Contacts, contactInfo(contact))
		}
		buckets = append(buckets, info)
	}

	writeJSON(writer, buckets)
}

func (h *apiHandler) dataStore(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	values := make([]dataStoreInfo, 0, len(h.shell.node.DataStore))
	for key, data := range h.shell.node.DataStore {
		values = append(values, dataStoreInfo{Key: key, Bytes: len(data)})
	}

	writeJSON(writer, values)
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

func (s *shell) nodeInfos() []nodeInfo {
	if len(s.peers) == 0 {
		return []nodeInfo{contactInfo(s.contact)}
	}

	nodes := make([]nodeInfo, 0, len(s.peers))
	for _, node := range s.peers {
		nodes = append(nodes, contactInfo(node.RoutingTable.Me()))
	}
	return nodes
}

func contactInfo(contact kademlia.Contact) nodeInfo {
	id := ""
	if contact.ID != nil {
		id = contact.ID.String()
	}
	return nodeInfo{
		ID:      id,
		Address: contact.Address,
	}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func hashID(text string) *kademlia.KademliaID {
	return kademlia.NewKademliaID(hashHex([]byte(text)))
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
