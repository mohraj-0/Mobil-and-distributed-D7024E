package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"d7024e/kademlia"
	"d7024e/metadata"
	"d7024e/registry"
	"github.com/spf13/cobra"
)

func loadOwners(filename string) (kademlia.OwnershipVerifier, error) {
	if filename == "" {
		return kademlia.DNSOwnershipVerifier{}, nil
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var owners map[string]string
	// Windows PowerShell may write UTF-8 JSON with a byte-order mark.
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if err := json.Unmarshal(data, &owners); err != nil {
		return nil, fmt.Errorf("owners file: %w", err)
	}
	records := map[string][]string{}
	for domain, encoded := range owners {
		if _, _, err := packageName(domain + ":owner"); err != nil {
			return nil, err
		}
		if _, err := metadata.DecodePublicKey(encoded); err != nil {
			return nil, fmt.Errorf("owner %s: %w", domain, err)
		}
		records[domain] = []string{"d7024e-ed25519=" + encoded}
	}
	return kademlia.DNSOwnershipVerifier{Resolver: kademlia.NewFakeTXTResolver(records)}, nil
}

func packageName(name string) (string, string, error) {
	parts := strings.Split(name, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] != strings.ToLower(parts[0]) || strings.HasSuffix(parts[0], ".") || strings.ContainsAny(name, " \t\r\n/\\") {
		return "", "", fmt.Errorf("expected canonical DOMAIN:PACKAGE")
	}
	return parts[0], parts[1], nil
}

func (s *shell) registry() *registry.Registry {
	return &registry.Registry{Store: s.node, OwnershipVerifier: s.node.OwnershipVerifier}
}

func (s *shell) history(domain, pkg string) (*kademlia.LatestPointer, []registry.HistoryEntry, error) {
	p, err := s.node.LookupLatest(domain, pkg)
	if err != nil {
		return nil, nil, err
	}
	history, err := s.registry().TraverseHistory(p.VersionRecordHash, domain, pkg)
	if err != nil {
		return nil, nil, err
	}
	return p, history, nil
}

func (s *shell) showHistory(out io.Writer, name string) error {
	domain, pkg, err := packageName(name)
	if err != nil {
		return err
	}
	_, history, err := s.history(domain, pkg)
	if err != nil {
		return fmt.Errorf("package %s: %w", name, err)
	}
	// Print only after the entire history has authenticated.
	fmt.Fprintf(out, "%s verified history (newest first)\n", name)
	for _, entry := range history {
		fmt.Fprintf(out, "  version=%d record=%s blob=%s prev=%s\n", entry.Record.Version, shortKey(entry.Hash), shortKey(entry.Record.BlobHash), predecessor(entry.Record.PreviousVersionRecord))
	}
	return nil
}

func (s *shell) showDNS(out io.Writer, domain string) error {
	if _, _, err := packageName(domain + ":owner"); err != nil {
		return err
	}
	if s.node.OwnershipVerifier == nil {
		return fmt.Errorf("dns %s: ownership verifier is not configured", domain)
	}
	key, err := s.node.OwnershipVerifier.ResolvePublicKey(domain)
	if err != nil {
		return fmt.Errorf("dns %s: no owner or ownership lookup error: %w", domain, err)
	}
	encoded, err := metadata.EncodePublicKey(key)
	if err != nil {
		return fmt.Errorf("dns %s: invalid owner public key: %w", domain, err)
	}
	fmt.Fprintf(out, "%s public-key=%s\n", domain, encoded)
	return nil
}

func predecessor(hash string) string {
	if hash == metadata.ZeroHash {
		return "genesis"
	}
	return shortKey(hash)
}

// This is a diagnostic decode, not an assertion that the record is trusted.
func displayStoredValue(out io.Writer, key string, data []byte) {
	var header struct {
		Tag string `json:"tag"`
	}
	_ = json.Unmarshal(data, &header)
	invalid := func(kind string) { fmt.Fprintf(out, "%s %s malformed bytes=%d\n", shortKey(key), kind, len(data)) }
	switch header.Tag {
	case metadata.VersionRecordTag:
		var r metadata.VersionRecord
		if json.Unmarshal(data, &r) != nil || !recordFields(r.Domain, r.Package, r.Signature) || !validKey(r.BlobHash) || r.BlobHash == metadata.ZeroHash || !validKey(r.PreviousVersionRecord) || hashHex(data) != key {
			invalid(header.Tag)
			return
		}
		fmt.Fprintf(out, "%s version-record %s:%s version=%d blob=%s prev=%s\n", shortKey(key), r.Domain, r.Package, r.Version, shortKey(r.BlobHash), predecessor(r.PreviousVersionRecord))
	case metadata.LatestPointerTag:
		var p metadata.LatestPointer
		if json.Unmarshal(data, &p) != nil || !recordFields(p.Domain, p.Package, p.Signature) || !validKey(p.VersionRecordHash) || p.VersionRecordHash == metadata.ZeroHash || key != kademlia.LatestKey(p.Domain, p.Package) {
			invalid(header.Tag)
			return
		}
		fmt.Fprintf(out, "%s latest-pointer %s:%s version=%d record=%s\n", shortKey(key), p.Domain, p.Package, p.Version, shortKey(p.VersionRecordHash))
	default:
		if hashHex(data) != key {
			invalid("value")
			return
		}
		fmt.Fprintf(out, "%s blob bytes=%d\n", shortKey(key), len(data))
	}
}

func recordFields(domain, pkg, signature string) bool {
	_, _, err := packageName(domain + ":" + pkg)
	sig, decodeErr := hex.DecodeString(signature)
	return err == nil && decodeErr == nil && len(sig) == ed25519.SignatureSize
}

func (s *shell) publishCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "publish DOMAIN:PACKAGE:VERSION FILENAME", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		force, prev := s.opts.force, s.opts.prev
		// Flags apply to this publication, including in a reused interactive root.
		s.opts.force, s.opts.prev = false, ""
		return s.publish(cmd.OutOrStdout(), args[0], args[1], force, prev)
	}}
	cmd.Flags().BoolVar(&s.opts.force, "force", defaultForce, "skip publisher preflight; receivers still validate updates")
	cmd.Flags().StringVar(&s.opts.prev, "prev", "", "explicit predecessor version (0 for genesis); with --force also accepts a record hash")
	return cmd
}

func versionName(name string) (string, string, string, error) {
	parts := strings.Split(name, ":")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("expected DOMAIN:PACKAGE:VERSION")
	}
	domain, pkg, err := packageName(strings.Join(parts[:2], ":"))
	return domain, pkg, parts[2], err
}

func pointerAbsent(err error) bool {
	var e *kademlia.LatestError
	return errors.As(err, &e) && e.Status == "missing_data" && strings.Contains(e.Detail, "pointer not found")
}

func (s *shell) publish(out io.Writer, name, filename string, force bool, prev string) error {
	domain, pkg, rawVersion, err := versionName(name)
	if err != nil {
		return err
	}
	version, err := strconv.ParseUint(rawVersion, 10, 64)
	if err != nil || version == 0 {
		return fmt.Errorf("publication version must be a positive integer")
	}
	if s.opts.publisherKey == "" {
		return fmt.Errorf("publish requires --publisher-key FILE")
	}
	encoded, err := os.ReadFile(s.opts.publisherKey)
	if err != nil {
		return err
	}
	sk, err := metadata.LoadPublisherPrivateKey(string(encoded))
	if err != nil {
		return err
	}
	if !force {
		if s.node.OwnershipVerifier == nil {
			return fmt.Errorf("ownership verifier is not configured")
		}
		owner, err := s.node.OwnershipVerifier.ResolvePublicKey(domain)
		if err != nil {
			return err
		}
		if !sk.Public().(ed25519.PublicKey).Equal(owner) {
			return fmt.Errorf("publisher key does not own %s", domain)
		}
	}
	head, history, err := s.history(domain, pkg)
	if err != nil && !pointerAbsent(err) {
		return err
	}
	previous := metadata.ZeroHash
	if head != nil {
		previous = head.VersionRecordHash
	}
	if prev != "" {
		previous = ""
		if prev == "0" {
			previous = metadata.ZeroHash
		}
		for _, entry := range history {
			if strconv.FormatUint(entry.Record.Version, 10) == prev {
				previous = entry.Hash
			}
		}
		if force && validKey(prev) {
			previous = prev
		}
		if previous == "" {
			return fmt.Errorf("predecessor version %s not found", prev)
		}
	}
	if !force && head != nil && (version <= head.Version || previous != head.VersionRecordHash) {
		return fmt.Errorf("publication must increase the version and extend the latest head")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	blob, err := s.registry().StoreBlob(data)
	if err != nil {
		return err
	}
	r := metadata.VersionRecord{Tag: metadata.VersionRecordTag, Domain: domain, Package: pkg, Version: version, BlobHash: blob, PreviousVersionRecord: previous}
	if err := metadata.SignVersionRecord(&r, sk); err != nil {
		return err
	}
	var hash string
	if force {
		encoded, err := json.Marshal(r)
		if err != nil {
			return err
		}
		hash, err = s.node.Store(encoded)
		if err != nil {
			return err
		}
	} else {
		hash, err = s.registry().StoreVersionRecord(&r)
		if err != nil {
			return err
		}
	}
	p := metadata.LatestPointer{Tag: metadata.LatestPointerTag, Domain: domain, Package: pkg, Version: version, VersionRecordHash: hash}
	if err := metadata.SignLatestPointer(&p, sk); err != nil {
		return err
	}
	var results map[string]error
	if force {
		results, err = s.node.PublishLatestForced(p)
	} else {
		results, err = s.node.PublishLatest(p)
	}
	addresses := make([]string, 0, len(results))
	for address := range results {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if results[address] == nil {
			fmt.Fprintf(out, "replica %s accepted\n", address)
		} else {
			fmt.Fprintf(out, "replica %s rejected: %v\n", address, results[address])
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "published %s record=%s blob=%s\n", name, hash, blob)
	return nil
}

func (s *shell) installCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "install DOMAIN:PACKAGE:VERSION", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		filename := output
		output = ""
		return s.install(cmd.OutOrStdout(), args[0], filename)
	}}
	cmd.Flags().StringVar(&output, "output", "", "destination file; default PACKAGE-VERSION.pkg")
	return cmd
}

func (s *shell) install(out io.Writer, name, filename string) error {
	domain, pkg, version, err := versionName(name)
	if err != nil {
		return err
	}
	if version != "latest" {
		if v, err := strconv.ParseUint(version, 10, 64); err != nil || v == 0 {
			return fmt.Errorf("install version must be a positive integer or latest")
		}
	}
	_, history, err := s.history(domain, pkg)
	if err != nil {
		return err
	}
	var selected *registry.HistoryEntry
	for i := range history {
		if version == "latest" || strconv.FormatUint(history[i].Record.Version, 10) == version {
			selected = &history[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("package %s version %s not found", domain+":"+pkg, version)
	}
	data, source, err := s.node.LookupDataFrom(selected.Record.BlobHash)
	if err != nil {
		return err
	}
	if hashHex(data) != selected.Record.BlobHash {
		return fmt.Errorf("package blob failed hash verification")
	}
	if filename == "" {
		filename = fmt.Sprintf("%s-%d.pkg", pkg, selected.Record.Version)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed %s:%s:%d to %s received from %s id=%s\n", domain, pkg, selected.Record.Version, filename, source.Address, shortID(source.ID))
	return nil
}

func keygenCmd() *cobra.Command {
	return &cobra.Command{Use: "keygen FILE", Short: "Create a local publisher key and print its public DNS TXT value", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		pk, sk, err := metadata.GeneratePublisherKeyPair()
		if err != nil {
			return err
		}
		encoded, err := metadata.EncodePublisherPrivateKey(sk)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(file, encoded+"\n")
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(cmd.OutOrStdout(), "d7024e-ed25519=%s\n", hex.EncodeToString(pk))
		return nil
	}}
}
