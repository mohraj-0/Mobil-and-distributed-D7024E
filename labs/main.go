// Package main starts a Kademlia node.
package main

import (
	"bufio"
	"crypto/sha256"
	"d7024e/kademlia"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// HTTP-handler för Store och LookupData.
type objectHandler struct {
	node *kademlia.Kademlia
}

func (oh *objectHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	switch request.Method {

	case "POST":
		scanner := bufio.NewScanner(
			request.Body,
		)

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				http.Error(
					writer,
					err.Error(),
					http.StatusBadRequest,
				)
			}
			return
		}

		data := scanner.Bytes()

		hash, err :=
			oh.node.Store(data)

		if err != nil {
			http.Error(
				writer,
				err.Error(),
				http.StatusInternalServerError,
			)
			return
		}

		writer.Header().Set(
			"Location",
			"/objects/"+hash,
		)

		writer.WriteHeader(
			http.StatusCreated,
		)

		_, _ = writer.Write(data)

	case "GET":
		parts :=
			strings.Split(
				request.URL.Path,
				"/",
			)

		if len(parts) != 3 {
			writer.WriteHeader(
				http.StatusNotFound,
			)
			return
		}

		hash := parts[2]

		data, err :=
			oh.node.LookupData(hash)

		if err != nil {
			writer.WriteHeader(
				http.StatusNotFound,
			)
			return
		}

		writer.WriteHeader(
			http.StatusOK,
		)

		_, _ = writer.Write(data)

	default:
		writer.WriteHeader(
			http.StatusMethodNotAllowed,
		)
	}
}

// Startar HTTP API.
func httpAPI(
	node *kademlia.Kademlia,
	port string,
) {
	handler :=
		&objectHandler{
			node: node,
		}

	mux :=
		http.NewServeMux()

	mux.Handle(
		"/objects",
		handler,
	)

	mux.Handle(
		"/objects/",
		handler,
	)

	err :=
		http.ListenAndServe(
			":"+port,
			mux,
		)

	if err != nil {
		log.Printf(
			"HTTP server stopped: %v\n",
			err,
		)
	}
}

// Skapar node-ID från IP:port med SHA-256.
func nodeIDFromAddress(
	address string,
) *kademlia.KademliaID {

	hash :=
		sha256.Sum256(
			[]byte(address),
		)

	return kademlia.NewKademliaID(
		hex.EncodeToString(
			hash[:],
		),
	)
}

func main() {
	// Kommandoradsargument.
	bootIP :=
		flag.String(
			"bip",
			"",
			"Bootstrap node IP",
		)

	bootID :=
		flag.String(
			"bid",
			"",
			"Bootstrap node ID",
		)

	port :=
		flag.String(
			"port",
			"8000",
			"Network port",
		)

	httpPort :=
		flag.String(
			"http",
			"8080",
			"HTTP port",
		)

	transport :=
		flag.String(
			"transport",
			"simulated",
			"Network transport: simulated or udp",
		)

	flag.Parse()

	// Hämta datorns/containerns hostname.
	hostname, err :=
		os.Hostname()

	if err != nil {
		log.Fatal(err)
	}

	address :=
		net.JoinHostPort(
			hostname,
			*port,
		)

	// Skapa ID = SHA-256(IP|port/address).
	id :=
		nodeIDFromAddress(
			address,
		)

	if id == nil {
		log.Fatal(
			"could not create node ID",
		)
	}

	fmt.Println(
		"Kademlia node address:",
		address,
	)

	fmt.Println(
		"Kademlia node ID:",
		id.String(),
	)

	// Skapa vald transport.
	var network kademlia.Node
	switch strings.ToLower(*transport) {
	case "simulated", "sim":
		network = kademlia.NewSimulatedNode()
	case "udp":
		network = kademlia.NewUDPNode()
	default:
		log.Fatal("transport must be simulated or udp")
	}

	// Starta vald transport.
	err =
		network.Listen(
			address,
		)

	if err != nil {
		log.Fatal(
			"Could not start transport:",
			err,
		)
	}

	defer network.Close()

	// Skapa vår egen Contact.
	me :=
		kademlia.NewContact(
			id,
			address,
		)

	// Skapa routing table.
	routingTable :=
		kademlia.NewRoutingTable(
			me,
		)

	// Skapa Kademlia.
	node :=
		&kademlia.Kademlia{
			RoutingTable: routingTable,
			Network:      network,

			// Standard enligt vår implementation.
			Alpha: 3,
			K:     10,

			RPCTimeout: 2 * time.Second,
		}

	// Starta mottagning av:
	// FIND_NODE
	// FIND_VALUE
	// STORE
	go node.ListenForRPC()

	// Om bip och bid anges är detta inte bootstrap-noden.
	if *bootIP != "" &&
		*bootID != "" {

		bootstrapID :=
			kademlia.NewKademliaID(
				*bootID,
			)

		if bootstrapID == nil {
			log.Fatal(
				"invalid bootstrap ID",
			)
		}

		bootstrapAddress :=
			net.JoinHostPort(
				*bootIP,
				"8000",
			)

		bootstrap :=
			kademlia.NewContact(
				bootstrapID,
				bootstrapAddress,
			)

		fmt.Println(
			"Joining network through:",
			bootstrapAddress,
		)

		err :=
			node.Join(
				bootstrap,
			)

		if err != nil {
			log.Printf(
				"Join failed: %v\n",
				err,
			)
		}
	} else {
		fmt.Println(
			"This node is the bootstrap node",
		)
	}

	// Starta HTTP API.
	go httpAPI(
		node,
		*httpPort,
	)

	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("put FILENAME")
	fmt.Println("get KEY [FILENAME]")
	fmt.Println("show ds")
	fmt.Println("exit")

	scanner :=
		bufio.NewScanner(
			os.Stdin,
		)

	for {
		fmt.Print("> ")

		if !scanner.Scan() {
			break
		}

		input :=
			strings.TrimSpace(
				scanner.Text(),
			)

		if input == "" {
			continue
		}

		parts :=
			strings.Fields(
				input,
			)

		switch parts[0] {

		// Avsluta noden.
		case "exit":
			fmt.Println(
				"Closing node",
			)

			return

		// put FILENAME
		case "put":
			if len(parts) != 2 {
				fmt.Println(
					"Usage: put FILENAME",
				)
				continue
			}

			data, err :=
				os.ReadFile(
					parts[1],
				)

			if err != nil {
				fmt.Println(
					"Could not read file:",
					err,
				)
				continue
			}

			hash, err :=
				node.Store(
					data,
				)

			if err != nil {
				fmt.Println(
					"Store failed:",
					err,
				)
				continue
			}

			fmt.Println(
				"Stored with key:",
				hash,
			)

		// get KEY [FILENAME]
		case "get":
			if len(parts) < 2 ||
				len(parts) > 3 {

				fmt.Println(
					"Usage: get KEY [FILENAME]",
				)
				continue
			}

			hash :=
				parts[1]

			// SHA-256 =
			// 32 bytes =
			// 64 hex characters.
			if len(hash) != 64 {
				fmt.Println(
					"Key must be 64 hex characters",
				)
				continue
			}

			data, err :=
				node.LookupData(
					hash,
				)

			if err != nil {
				fmt.Println(
					"Data not found:",
					err,
				)
				continue
			}

			// Om filename anges,
			// spara datan i filen.
			if len(parts) == 3 {
				err :=
					os.WriteFile(
						parts[2],
						data,
						0644,
					)

				if err != nil {
					fmt.Println(
						"Could not save file:",
						err,
					)
					continue
				}

				fmt.Println(
					"Data saved to:",
					parts[2],
				)

				continue
			}

			// Annars skriv datan.
			fmt.Println(
				"Data:",
				string(data),
			)

		// Visa lokal data store.
		case "show":
			if len(parts) != 2 {
				fmt.Println(
					"Usage: show ds",
				)
				continue
			}

			if parts[1] == "ds" {
				fmt.Println(
					"Local data store:",
				)

				for key := range node.DataStore {

					fmt.Println(
						key,
					)
				}

				continue
			}

			fmt.Println(
				"Unknown show command",
			)

		default:
			fmt.Println(
				"Invalid command:",
				parts[0],
			)
		}
	}
}
