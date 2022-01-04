package blockchair

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

const (
	BaseUrl        = "https://api.blockchair.com/"
	DefaultTimeout = 10 * time.Second
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
	path := fmt.Sprintf("%s/dashboards/address/%s", b.config.BaseURL, addr)

	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Body: %s\n", body)
	fmt.Printf("Response status: %s\n", resp.Status)

	addrStats := AddressStatsResponse{
		Data: map[string]*AddressStats{
			addr: &AddressStats{}, // payload value is keyed by its public key address
		},
	}

	if err := json.Unmarshal(body, &addrStats); err != nil {
		return nil, err
	}

	fmt.Printf("unmarshalled struct: %v\n", addrStats)

	return &addrStats, nil
}
