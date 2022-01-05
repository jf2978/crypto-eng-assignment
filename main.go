package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
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

// AddResponse represents the expected response body to '/add'
type AddResponse struct {
	Address *AddressesRecord `json:"address"`
}

// BalanceRequest represents the expected request body to '/balance'
type BalanceRequest struct {
	Address string `json:"address"`
}

// BalanceResponse represents the expected request body to '/balance'
type BalanceResponse struct {
	Balance float64 `json:"balance"`
}

// TransactionsRequest represents the expected request body to '/transactions'
type TransactionsRequest struct {
	Address string `json:"address"`
}

// TransactionsResponse represents the expected response body to '/transactions'
type TransactionsResponse struct {
	Transactions []*TransactionsRecord `json:"transactions"`
}

// SyncRequest represents the expected request body to '/sync'
type SyncRequest struct {
	Address string `json:"address"`
}

// SyncResponse represents the expected response body to '/sync'
type SyncResponse struct {
	Address *AddressesRecord `json:"address"`
}

// DetectTransfersRequest represents the expected request body to '/detect-transfers'
type DetectTransfersRequest struct {
	Txns []*CustomTxn `json:"transactions"`
}

// CustomTxn represents the dummy transaction data used to demonstrate how we might detect transfers between wallets
type CustomTxn struct {
	TxnID        string    `json:"id"`
	WalletID     string    `json:"wallet"`
	TxnTimestamp time.Time `json:"time"`
	TxnFlow      string    `json:"flow"` // determines whether the txn is flow "in" or "out" of the wallet specified
	AmountUSD    float64   `json:"amount"`
}

// UnmarshalJSON implements the Unmarshaler interface and overrides the default behavior in encoding/json
// in order to accurately convert timestamps to a Go time.Time
func (c *CustomTxn) UnmarshalJSON(data []byte) error {

	fmt.Print("custom marshaller!")
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		fmt.Printf("error%v\n", err)

		return err
	}

	c.TxnID = v["id"].(string)
	c.WalletID = v["wallet"].(string)
	c.TxnFlow = v["flow"].(string)
	c.AmountUSD = v["amount"].(float64)

	rawTime, err := time.Parse("2006-01-02 15:04:05 UTC", v["time"].(string))
	if err != nil {
		return err
	}
	c.TxnTimestamp = rawTime

	return nil
}

// AddressesRecord is the data model for a respective row in the 'addresses' table stored in Spanner
type AddressesRecord struct {
	PublicKey   string    `spanner:"public_key"`
	Balance     float64   `spanner:"balance"`
	CreatedAt   time.Time `spanner:"created_at"`
	UpdatedAt   time.Time `spanner:"updated_at"`
	LastTxnHash string    `spanner:"last_txn_hash"`
}

// TransactionsRecord is the data model for a respective row in the 'transactions' table stored in Spanner
type TransactionsRecord struct {
	TxnHash      string    `spanner:"txn_hash"`   // pk
	PublicKey    string    `spanner:"public_key"` // pk
	Amount       float64   `spanner:"amount"`
	Fee          float64   `spanner:"fee"`
	Tags         string    `spanner:"tags"`
	TxnTimestamp time.Time `spanner:"txn_timestamp"`
	CreatedAt    time.Time `spanner:"created_at"`
}

const (
	// server configuration
	endpoint = "localhost"
	port     = "8080"

	// todo: replace with better names & use environment variables in the real world
	projectID  = "cointracker-test-1234"
	instanceID = "test-instance"
	databaseID = "test-db"

	// tables
	addressesTable    = "addresses"
	transactionsTable = "transactions"
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
	r.Handle("/add", AddHandler(ctx, spannerClient, blockchairClient))
	r.Handle("/balance", GetBalanceHandler(ctx, spannerClient, blockchairClient))
	r.Handle("/transactions", GetTransactionsHandler(ctx, spannerClient, blockchairClient))
	r.Handle("/sync", SyncHandler(ctx, spannerClient, blockchairClient))
	r.Handle("/detect-transfer", DetectTransfersHandler(ctx, spannerClient))

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

		address, err := add(ctx, addReq.Address, s, b)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		addrResp := &AddResponse{Address: address}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(addrResp)
	})
}

// add adds a BTC wallet if it doesn't already exist and imports its associated transactions
func add(ctx context.Context, addr string, s *spanner.Client, b *blockchair.Client) (*AddressesRecord, error) {
	address := &AddressesRecord{}

	_, err := s.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {

		row, err := txn.ReadRow(ctx, addressesTable, spanner.Key{addr}, []string{"public_key", "balance", "last_txn_hash", "created_at", "updated_at"})

		// this address already exists in the addresses table, we're done
		if err == nil {
			var addrRec AddressesRecord
			if structErr := row.ToStruct(&addrRec); structErr != nil {
				return structErr
			}

			address = &addrRec
			return nil
		}

		if err != nil && spanner.ErrCode(err) != codes.NotFound {
			return err
		}

		// create this address & immediately sync transactions relevant to this address
		address, _, err = sync(ctx, txn, b, addr, "")
		if err != nil {
			return err
		}

		return nil
	})

	return address, err
}

// GetBalanceHandler returns a closure responsible for validating the incoming request
// and invoking balance() to fetch the provided address' balance
func GetBalanceHandler(ctx context.Context, s *spanner.Client, b *blockchair.Client) http.Handler {
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

		b, err := balance(ctx, s, b, balanceReq.Address)

		if err != nil {
			http.Error(w, fmt.Sprintf("could not get balance for address %s\n. %v", balanceReq.Address, err), http.StatusInternalServerError)
			return
		}

		balanceResp := &BalanceResponse{Balance: b}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(balanceResp)
	})
}

// balance gets the current balance of the give BTC address
// note: the returned value can be out of date, if we want the most up-to-date balance, we have to call `sync` first
func balance(ctx context.Context, s *spanner.Client, b *blockchair.Client, addr string) (float64, error) {
	var addressRec *AddressesRecord

	_, err := s.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		row, readErr := txn.ReadRow(ctx, addressesTable, spanner.Key{addr}, []string{"last_txn_hash"})
		if readErr != nil {
			return readErr
		}

		var lastTxnHash string
		row.ColumnByName("last_txn_hash", &lastTxnHash)

		address, _, syncErr := sync(ctx, txn, b, addr, lastTxnHash)
		if syncErr != nil {
			return syncErr
		}

		addressRec = address

		return nil
	})

	if err != nil {
		return 0, err
	}

	return addressRec.Balance, nil
}

// GetTransactionsHandler returns a closure responsible for validating the incoming request
// and invoking transactions() to fetch the provided address' list of all transactions
func GetTransactionsHandler(ctx context.Context, s *spanner.Client, b *blockchair.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var txnsReq TransactionsRequest
		if err := json.Unmarshal(body, &txnsReq); err != nil {
			http.Error(w, "provided payload is not valid JSON", http.StatusBadRequest)
			return
		}

		txnsRecs, err := transactions(ctx, txnsReq.Address, s, b)

		if err != nil {
			http.Error(w, fmt.Sprintf("could not get transactions for address %s\n. %v", txnsReq.Address, err), http.StatusInternalServerError)
			return
		}

		txnsResp := &TransactionsResponse{Transactions: txnsRecs}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(txnsResp)
	})
}

// transactions gets the current transactions associated with the provided BTC address (mapped key-value "txn_hash" -> txn struct)
// limitations: the first time this is called for an address, historical transactions may take a while to be fetched and
func transactions(ctx context.Context, addr string, s *spanner.Client, b *blockchair.Client) ([]*TransactionsRecord, error) {
	var txnsRecs []*TransactionsRecord

	_, err := s.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {

		// read for transactions associated with this address
		stmt := spanner.NewStatement(`
			SELECT t.txn_hash, t.public_key, t.amount, t.fee, t.tags, t.txn_timestamp, a.last_txn_hash
			FROM transactions t
			JOIN addresses a USING(public_key)
			WHERE public_key = @address
		`)
		stmt.Params["address"] = addr

		lastTxnHash := ""

		iter := txn.Query(ctx, stmt)
		iterErr := iter.Do(func(row *spanner.Row) error {

			type JoinedRecord struct {
				TxnHash      string    `spanner:"txn_hash"`
				PublicKey    string    `spanner:"public_key"`
				Amount       float64   `spanner:"amount"`
				Fee          float64   `spanner:"fee"`
				Tags         string    `spanner:"tags`
				TxnTimestamp time.Time `spanner:"txn_timestamp"`
				LastTxnHash  string    `spanner:"last_txn_hash"`
			}

			var rec JoinedRecord
			row.ToStruct(&rec)

			txnsRecs = append(txnsRecs, &TransactionsRecord{
				PublicKey:    rec.PublicKey,
				Amount:       rec.Amount,
				Fee:          rec.Fee,
				Tags:         rec.Tags,
				TxnTimestamp: rec.TxnTimestamp,
			})

			fmt.Printf("joined struct: %+v\n", rec)

			if len(lastTxnHash) == 0 {
				lastTxnHash = rec.LastTxnHash
			}

			return nil
		})

		if iterErr != nil {
			return iterErr
		}

		fmt.Printf("last txn hash found: %s\n", lastTxnHash)

		_, txns, syncErr := sync(ctx, txn, b, addr, lastTxnHash)
		if syncErr != nil {
			return syncErr
		}

		txnsRecs = append(txnsRecs, txns...)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return txnsRecs, nil
}

// SyncHandler returns a closure responsible for validating the incoming request
// and invoking sync() to trigger an update for the provided address (and its transactions)
func SyncHandler(ctx context.Context, s *spanner.Client, b *blockchair.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var syncReq SyncRequest
		if err := json.Unmarshal(body, &syncReq); err != nil {
			http.Error(w, "provided payload is not valid JSON", http.StatusBadRequest)
			return
		}

		addr := syncReq.Address

		var syncResp *SyncResponse
		_, err = s.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
			row, readErr := txn.ReadRow(ctx, addressesTable, spanner.Key{addr}, []string{"last_txn_hash"})
			if readErr != nil {
				return readErr
			}

			var lastTxnHash string
			row.ColumnByName("last_txn_hash", &lastTxnHash)

			addressRec, _, syncErr := sync(ctx, txn, b, addr, lastTxnHash)
			if syncErr != nil {
				return syncErr
			}

			syncResp = &SyncResponse{
				Address: addressRec,
			}

			return nil
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(syncResp)
	})
}

// sync fetches the latest address & transaction data from the blockchair API
func sync(ctx context.Context, txn *spanner.ReadWriteTransaction, b *blockchair.Client, addr, lastTxnHash string) (*AddressesRecord, []*TransactionsRecord, error) {
	var address *AddressesRecord
	var transactions []*TransactionsRecord

	now := time.Now()

	// pull the latest transaction data for this address
	addrStats, err := getAddrStats(ctx, b, addr)

	if err != nil {
		return nil, nil, err
	}

	txnHashes := addrStats.Txns

	// find the cutoff point and filter out txn hashes that we've already seen/processed if we know it
	if len(lastTxnHash) > 0 {
		for i, v := range addrStats.Txns {
			if v == lastTxnHash {
				txnHashes = txnHashes[:i]
				break
			}
			fmt.Printf("skipping syncing transaction: %s\n", v)
		}
	}

	txns, err := getTransactions(ctx, b, txnHashes)
	if err != nil {
		return nil, nil, err
	}

	// insert the transaction data into our tables
	mutations := []*spanner.Mutation{}
	for _, v := range txns {
		rec := &TransactionsRecord{
			TxnHash:      v.Txn.Hash,
			PublicKey:    addr,
			Amount:       v.Txn.AmountUSD,
			Fee:          v.Txn.FeeUSD,
			TxnTimestamp: v.Txn.Timestamp,
			CreatedAt:    now,
		}

		transactions = append(transactions, rec)

		mut, err := spanner.InsertStruct(transactionsTable, rec)
		if err != nil {
			return nil, nil, err
		}

		mutations = append(mutations, mut)
	}

	// update the addresses table to match newest data returned by our api (primarily balance + last_txn_hash)
	address = &AddressesRecord{
		PublicKey:   addr,
		Balance:     addrStats.Addr.BalanceUSD,
		CreatedAt:   now,
		UpdatedAt:   now,
		LastTxnHash: addrStats.Txns[0],
	}

	mut, err := spanner.InsertOrUpdateStruct(addressesTable, address)
	if err != nil {
		return nil, nil, err
	}

	mutations = append(mutations, mut)

	if err := txn.BufferWrite(mutations); err != nil {
		return nil, nil, err
	}

	return address, transactions, err
}

// DetectTransfersHandler returns a closure responsible for validating the incoming request
// and invoking detectTransfers() to tag transactions that are likely transfers
func DetectTransfersHandler(ctx context.Context, s *spanner.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var detectReq DetectTransfersRequest
		if err := json.Unmarshal(body, &detectReq); err != nil {
			http.Error(w, "provided payload is not valid JSON", http.StatusBadRequest)
			return
		}

		fmt.Printf("detect trasnfers request %v\n", detectReq)

		txns, err := detectTransfers(detectReq.Txns)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(txns)
	})
}

// detectTransfers detects the likely transfers between a user's wallets with fuzzy matching based on transaction
// amounts and corresponding timestamps
func detectTransfers(txns []*CustomTxn) (map[string]string, error) {
	// todo: (nice to have) group addresses into "user wallets" and save them to the users table

	// brute force -> compare all possible pairs w/ nested iteration. O(1) space, but O(n^2) time (no bueno)

	// alternative (sorting) -> sort transactions by timestamp, for each timestamp/range bucket,
	// look for out/in pairs s.t. amounts & wallets are equivalent. O(n) space and O(nlogn) time where n = # of transactions

	// simpler version to start can assume timestamps are exact matches
	// can be extended to use buckets and time ranges instead

	/* for _, v := range txns {
		fmt.Printf("unsorted transaction: %+v\n", v)
	} */

	// sort transactions by timestamp and amount if timestamps are equal
	sort.Slice(txns, func(i, j int) bool {
		if txns[i].TxnTimestamp.Before(txns[j].TxnTimestamp) {
			return true
		}

		if txns[i].TxnTimestamp.Equal(txns[j].TxnTimestamp) {
			return txns[i].AmountUSD < txns[j].AmountUSD
		}

		return false
	})

	/* for _, v := range txns {
		fmt.Printf("sorted transaction: %+v\n", v)
	} */

	return nil, nil

	// todo: (nice to have) save this detected and add tag to transactions by hash id + this address (composite key)
}

// getAddrStats gets the AddressStats for the provided address via the Blockchair API
func getAddrStats(ctx context.Context, b *blockchair.Client, addr string) (*blockchair.AddressStats, error) {
	statsResp, err := b.GetAddressStats(ctx, addr)

	if err != nil {
		return nil, err
	}

	return statsResp.Data[addr], nil
}

// getTransactions gets all transaction data for provided txn hashes via the Blockchair API (in batches of 10)
// note: the blockchair api limits calls to their /transactions endpoint for up to 10 txn hashes
// ideally, we'd parallelize these chunks and/or kick off a job instead of blocking execution
func getTransactions(ctx context.Context, b *blockchair.Client, txnHashes []string) (map[string]*blockchair.TransactionWrapper, error) {
	txns := make(map[string]*blockchair.TransactionWrapper)

	// group the list of all transactions into chunks of 10
	var batches [][]string
	for i := 0; i < len(txnHashes); i += 10 {
		end := i + 10

		if end > len(txnHashes) {
			end = len(txnHashes)
		}

		batches = append(batches, txnHashes[i:end])
	}

	// for each batch, get the transaction data
	for _, batch := range batches {
		fmt.Printf("processing batch: %v ...\n", batch)

		resp, err := b.GetTransactionsByHashes(ctx, batch)
		if err != nil {
			return nil, err
		}

		fmt.Printf("processed batch: %+v ...\n", resp.Data)

		txns = mergeTxnMaps(txns, resp.Data)
	}

	return txns, nil
}

// mergeTxnMaps merges the two provided maps of ("txn_hash" -> txn struct) into one
func mergeTxnMaps(a, b map[string]*blockchair.TransactionWrapper) map[string]*blockchair.TransactionWrapper {
	for k, v := range b {
		a[k] = v
	}

	return a
}

func main() {
	server := InitServer()

	log.Printf("Listening on port %s...\n", port)
	log.Fatal(http.ListenAndServe(
		fmt.Sprintf("%s:%s", endpoint, port), server.router),
	)
}
