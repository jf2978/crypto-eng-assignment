package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/gorilla/mux"
	"github.com/jf2978/cointracker-eng-assignment/blockchair"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
)

// Server represents a basic web server backed by a Google Spanner as a data store
type Server struct {
	context    context.Context
	router     *mux.Router
	spanner    *spanner.Client
	blockchair *blockchair.Client
}

// AddRequest represents the expected request body to '/add'
type AddRequest struct {
	Address string `json:"address"`
}

// BalanceRequest represents the expected request body to '/balance'
type BalanceRequest struct {
	Address string `json:"address"`
}

type AddressesRecord struct {
	PublicKey   string    `spanner:"public_key"`
	Balance     float64   `spanner:"balance"`
	CreatedAt   time.Time `spanner:"created_at"`
	UpdatedAt   time.Time `spanner:"updated_at"`
	LastTxnHash string    `spanner:"last_txn_hash"`
}

const (
	// server configuration
	address = "localhost"
	port    = "8080"

	// todo: replace with better names & use environment variables in the real world
	projectID  = "cointracker-test-1234"
	instanceID = "test-instance"
	databaseID = "test-db"

	// tables
	addressesTable = "addresses"
)

// InitServer returns a new Server with some default values
func InitServer() *Server {
	ctx := context.Background()

	// todo: faciliate later testing by setting spanner emulator env vars here

	dbPath := fmt.Sprintf("projects/%s/instances/%s/databases/%s", projectID, instanceID, databaseID)
	spannerClient, err := spanner.NewClient(ctx, dbPath, option.WithServiceAccountFile("./service-account.json"))
	if err != nil {
		log.Fatal(err)
	}

	blockchairClient := blockchair.NewClient(ctx)

	r := mux.NewRouter()

	// todo: rename routes to better reflect specific functionality (i.e. we're only handling btc addresses)
	r.Handle("/add", AddHandler(ctx, spannerClient, blockchairClient))
	r.Handle("/balance", GetBalanceHandler(ctx, spannerClient))

	return &Server{
		context:    ctx,
		router:     r,
		spanner:    spannerClient,
		blockchair: blockchairClient,
	}
}

// AddHandler returns a closure responsible for validating the incoming request
// and invoking add() to create a new BTC address
func AddHandler(ctx context.Context, s *spanner.Client, b *blockchair.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var addReq AddRequest
		if err := json.Unmarshal(body, &addReq); err != nil {
			http.Error(w, "provided payload is not valid JSON", http.StatusBadRequest)
			return
		}

		// super naive check to see if address is between 25 and 34 characters long
		if len(addReq.Address) < 25 || len(addReq.Address) > 34 {
			http.Error(w, "provided BTC address is not valid", http.StatusBadRequest)
			return
		}

		if err := add(ctx, addReq.Address, s, b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}

// add adds a BTC wallet if it doesn't already exist and imports its associated transactions
func add(ctx context.Context, addr string, s *spanner.Client, b *blockchair.Client) error {
	_, err := s.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {

		now := time.Now()
		_, err := txn.ReadRow(ctx, addressesTable, spanner.Key{addr}, []string{"public_key", "created_at", "updated_at"})

		// this address already exists in the addresses table, no-op
		if err == nil {
			return nil
		}

		if err != nil && spanner.ErrCode(err) != codes.NotFound {
			return err
		}

		// create this new address
		var mut *spanner.Mutation
		if spanner.ErrCode(err) == codes.NotFound {
			statsResp, err := b.GetAddressStats(ctx, addr)

			if err != nil {
				return err
			}

			fmt.Printf("stats payload: %v\n", statsResp.Data[addr])
			addressStats := statsResp.Data[addr]
			rec := &AddressesRecord{
				PublicKey:   addr,
				Balance:     addressStats.Addr.BalanceUSD,
				CreatedAt:   now,
				UpdatedAt:   now,
				LastTxnHash: addressStats.Txns[0],
			}

			mut, err = spanner.InsertStruct(addressesTable, rec)
			if err != nil {
				return err
			}
		}

		// todo: automatically sync transactions relevant to this address

		return txn.BufferWrite([]*spanner.Mutation{mut})
	})

	return err
}

// GetBalanceHandler returns a closure responsible for validating the incoming request
// and invoking balance() to fetch the provided address' balance
func GetBalanceHandler(ctx context.Context, s *spanner.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var balanceReq BalanceRequest
		if err := json.Unmarshal(body, &balanceReq); err != nil {
			http.Error(w, "provided payload is not valid JSON", http.StatusBadRequest)
			return
		}

		b, err := balance(ctx, s, balanceReq.Address)

		if err != nil {
			http.Error(w, fmt.Sprintf("could not get balance for address %s\n. %v", balanceReq.Address, err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(b)
	})
}

// balance gets the current balance of the give BTC address
// note: the returned value can be out of date, if we want the most up-to-date balance, we have to call `sync` first
func balance(ctx context.Context, s *spanner.Client, addr string) (float64, error) {
	row, err := s.Single().ReadRow(ctx, addressesTable, spanner.Key{addr}, []string{"balance"})

	if err != nil {
		return 0, err
	}

	var rec AddressesRecord
	if err := row.ToStruct(&rec); err != nil {
		return 0, err
	}

	return rec.Balance, nil
}

func main() {
	server := InitServer()

	log.Printf("Listening on port %s...\n", port)
	log.Fatal(http.ListenAndServe(
		fmt.Sprintf("%s:%s", address, port), server.router),
	)
}
