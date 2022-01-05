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
	TransactionLimit = 100 // maximum allowed by the Blockchair API for dashborad/address endpoints
)

type Config struct {
	BaseURL string
}

// Blockchair represents a minimal http client that interacts with the Blockchair API
type Client struct {
	config *Config
	client *http.Client
}

type AddressStatsResponse struct {
	Data map[string]*AddressStats `json:"data"`
}

type AddressStats struct {
	Addr *Address `json:"address"`
	Txns []string `json:"transactions"`
}

type Address struct {
	AddressType string  `json:"type"`
	Balance     int     `json:"balance"`
	BalanceUSD  float64 `json:"balance_usd"`
}

type TransactionsResponse struct {
	Data map[string]*TransactionWrapper `json:"data"`
}

type TransactionWrapper struct {
	Txn *Transaction `json:"transaction"`
}

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

// GetTransactions queries the Blockchair API for transaction data by a list ids (hashes)
// todo: parallelize me using goroutines / channels to speed up API consumption, though note maps are not goroutine-safe; see https://go.dev/blog/maps
func (b *Client) GetTransactionsByHashes(ctx context.Context, txnHashes []string) (*TransactionsResponse, error) {

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
