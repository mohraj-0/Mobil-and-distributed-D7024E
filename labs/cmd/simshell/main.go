package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"d7024e/kademlia"
	"github.com/spf13/cobra"
)

const localAddress = "127.0.0.1:8000"

type shell struct {
	addr    string
	contact kademlia.Contact
	rt      *kademlia.RoutingTable
	store   map[string][]byte
}

func main() {
	s := newShell(localAddress)
	cmd := s.root()
	if len(os.Args) > 1 {
		cmd.SetArgs(os.Args[1:])
		if err := cmd.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	s.repl()
}

func newShell(addr string) *shell {
	contact := kademlia.NewContact(hashID(addr), addr)
	return &shell{
		addr:    addr,
		contact: contact,
		rt:      kademlia.NewRoutingTable(contact),
		store:   make(map[string][]byte),
	}
}

func (s *shell) repl() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("simshell node %s id=%s\n", s.addr, shortID(s.contact.ID))
	for {
		fmt.Print("simnet> ")
		if !scanner.Scan() {
			fmt.Println()
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

func (s *shell) root() *cobra.Command {
	root := &cobra.Command{Use: "simshell", SilenceUsage: true, SilenceErrors: true}
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
		s.showCmd(),
		&cobra.Command{Use: "exit", Aliases: []string{"quit"}, Args: cobra.NoArgs, Run: func(*cobra.Command, []string) {
			os.Exit(0)
		}},
	)
	return root
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
	)
	return show
}

func (s *shell) ping(rawAddr string) error {
	addr, err := parseAddress(rawAddr)
	if err != nil {
		return err
	}
	start := time.Now()
	if addr != s.addr {
		return fmt.Errorf("no simulated node at %s", addr)
	}
	fmt.Printf("pong from %s in %s\n", addr, time.Since(start))
	return nil
}

func (s *shell) put(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	key := hashHex(data)
	s.store[key] = data
	fmt.Println(key)
	return nil
}

func (s *shell) get(key string, filename string) error {
	if !validKey(key) {
		return fmt.Errorf("key must be a %d-character hex SHA-256 hash", kademlia.IDLength*2)
	}
	data, ok := s.store[strings.ToLower(key)]
	if !ok {
		return fmt.Errorf("key %q not found", key)
	}
	if hashHex(data) != strings.ToLower(key) {
		return fmt.Errorf("stored value failed hash verification")
	}
	if filename != "" {
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return err
		}
		fmt.Printf("value from %s saved to %s\n", s.addr, filename)
		return nil
	}
	fmt.Printf("value from %s: %s\n", s.addr, string(data))
	return nil
}

func (s *shell) show(what string) error {
	switch what {
	case "rt":
		for _, index := range s.rt.NonEmptyBucketIndices() {
			contacts := s.rt.ContactsInBucket(index)
			fmt.Printf("bucket %d: %d contacts\n", index, len(contacts))
			for _, contact := range contacts {
				fmt.Printf("  %s %s\n", shortID(contact.ID), contact.Address)
			}
		}
		if len(s.rt.NonEmptyBucketIndices()) == 0 {
			fmt.Println("routing table is empty")
		}
	case "ds":
		if len(s.store) == 0 {
			fmt.Println("data store is empty")
			return nil
		}
		for key, data := range s.store {
			fmt.Printf("%s bytes=%d\n", shortKey(key), len(data))
		}
	default:
		return fmt.Errorf("usage: show rt|ds")
	}
	return nil
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
	host, port, ok := strings.Cut(raw, ":")
	if !ok || host == "" {
		return "", fmt.Errorf("expected IP:PORT")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	return raw, nil
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
