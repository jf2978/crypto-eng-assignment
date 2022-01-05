package blockchair

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

const (
	BaseUrl          = "https://api.blockchair.com/"
	DefaultTimeout   = 10 * time.Second
	TransactionLimit = 50 // maximum allowed by the Blockchair API for dashborad/address endpoints
)

// Config represents the Blockchair API client configuration
type Config struct {
	BaseURL string
}

// Client represents a minimal http client that interacts with the Blockchair API
type Client struct {
	config *Config
	client *http.Client
}

// AddressStatsResponse represents the top-level envelope we expect from the address stats endpoint
type AddressStatsResponse struct {
	Data map[string]*AddressStats `json:"data"`
}

// AddressStats represents the primary payload we expect from the address stats endpoint
type AddressStats struct {
	Addr *Address `json:"address"`
	Txns []string `json:"transactions"`
}

// Address represents a minimal BTC address object
type Address struct {
	AddressType string  `json:"type"`
	Balance     int     `json:"balance"`
	BalanceUSD  float64 `json:"balance_usd"`
}

// TransactionsResponse represents the top-level envelope we expect from the transactions stats endpoint (batched)
type TransactionsResponse struct {
	Data map[string]*TransactionWrapper `json:"data"`
}

// TransactionWrapper represents the top-level envelope from the transactions stats endpoint (single)
type TransactionWrapper struct {
	Txn *Transaction `json:"transaction"`
}

// Transaction represents a minimal BTC transaction object
type Transaction struct {
	Hash      string    `json:"hash"`
	Timestamp time.Time `json:"time"`
	AmountUSD float64   `json:"output_total_usd"`
	FeeUSD    float64   `json:"fee_usd"`
}

// UnmarshalJSON implements the Unmarshaler interface and overrides the default behavior in encoding/json
// in order to accurately convert timestamps received by the Blockchair API to a Go time.Time
func (t *Transaction) UnmarshalJSON(data []byte) error {
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		fmt.Printf("error%v\n", err)

		return err
	}

	t.Hash = v["hash"].(string)
	t.AmountUSD = v["output_total_usd"].(float64)
	t.FeeUSD = v["fee_usd"].(float64)

	rawTime, err := time.Parse("2006-01-02 15:04:05", v["time"].(string))
	if err != nil {
		return err
	}
	t.Timestamp = rawTime

	return nil
}

// NewClient constructs a new Blockchair client
func NewClient(ctx context.Context) *Client {
	return &Client{
		client: &http.Client{
			Timeout: DefaultTimeout,
		},
		config: &Config{
			BaseURL: "https://api.blockchair.com/bitcoin",
		},
	}
}

// GetAddressStats queries the Blockchair API for a snapshot view of a given BTC address
func (b *Client) GetAddressStats(ctx context.Context, addr string) (*AddressStatsResponse, error) {
	path := fmt.Sprintf("%s/dashboards/address/%s?limit=%d", b.config.BaseURL, addr, TransactionLimit)

	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	addrStats := AddressStatsResponse{
		Data: map[string]*AddressStats{
			addr: &AddressStats{}, // payload value is keyed by its public key address
		},
	}

	if err := json.Unmarshal(body, &addrStats); err != nil {
		return nil, err
	}

	return &addrStats, nil
}

// GetTransactionsByHashes queries the Blockchair API for transaction data by a list ids (hashes)
func (b *Client) GetTransactionsByHashes(ctx context.Context, txnHashes []string) (*TransactionsResponse, error) {

	// todo: parallelize me using goroutines / channels to speed up API consumption, though note maps are not goroutine-safe; see https://go.dev/blog/maps

	// the blockchair API limits these requests to 10
	if len(txnHashes) > 10 {
		return nil, fmt.Errorf("cannot process more than 10 txn hashes at a time")
	}

	path := fmt.Sprintf("%s/dashboards/transactions/%s", b.config.BaseURL, strings.Join(txnHashes, ","))
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// blockchair has a hard limit of 30 reqs/minute, returning a 402 when that limit is reached
	// so this bootstraps some retry logic to retry on that status code
	// todo: remove this & pay for the API if I ever need to use this in irl
	if resp.StatusCode == 402 {
		fmt.Printf("waiting for the Blockchair API to cool down...\n")

		time.Sleep(time.Minute * 1)

		resp, err = http.Get(path)
		if err != nil {
			return nil, err
		}

		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	}

	txnsResp := TransactionsResponse{
		Data: map[string]*TransactionWrapper{},
	}

	if err := json.Unmarshal(body, &txnsResp); err != nil {
		return nil, err
	}

	return &txnsResp, nil
}
