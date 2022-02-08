# Crypto Engineering Assignment

## Goals

### Immediate Requirements
- Add bitcoin addresses
- View the current balance for a given bitcoin address
- View the historical transactions for a given bitcoin address
- Synchronize data to retrieve the latest balances and transactions on each bitcoin address
- Detect transfers between the user's addresses

### Future Goals ("Nice-to-haves")

These are some things that I'll keep in mind (both as potential considerations for designing the system and for neat additions that weren't explicitly asked for). Not the focus here, but helpful to enumerate these nonetheless.

- Allow for a user to give each address a nickname
- Transactionalize create/update endpoints where applicable (best practice)
- Add a minimal frontend
- Table indices: read `transactions` by from/to address -- oiptimization for read-heavy table where we'd likely want to read by a specific wallet address
- Implement syncing transactions on a user level (rather than by specific address)
- Add a CLI helper to interact with these endpoints without using something like `curl`

---

## Data Storage

Transactions, wallets, users, etc. are naturally relational and the structure table schemas give us will come in handy to flexibility stich together a bunch of different user activity we've read from the blockchain(s). Also, Google Spanner's ACID property guarantees will come in handy for transactionalizing read/writes across different tables so I'll try to use that here with minimal overhead (though I'm probably a little biased on this front).

### Tables

The `users` table would be responsible for storing a naive implementation of a user for this web app. This ended up not being used, but would prove useful in more realistic circumstances!

| field     | type       | description                       |
|-----------|------------|-----------------------------------|
| uuid (pk) | STRING MAX | unique identifier for this user   |
| username  | STRING MAX | human-readable name for this user |
| addresses | STRING MAX | comma-delimited list of addresses |

note: the MAX keyword specifies no "hard limit" for a given field, but is internally [optimized](https://stackoverflow.com/questions/45964937/performance-difference-for-stringmax) to store the limited length bytes

The `addresses` table stores the public key addresses added to the app

| field         | type       | description                                                 |
|---------------|------------|-------------------------------------------------------------|
| public_key(pk)| STRING MAX | the public key of this address                              |
| balance       | FLOAT64    | the amount stored at this address                           |
| created_at    | TIMESTAMP  | the point in time this record was created (UTC)             |
| updated_at    | TIMESTAMP  | the point in time this record was last updated (UTC)        |
| last_txn_hash | STRING MAX | the most recent transaction hash associated to this address |

The `transactions` table is responsible for storing blockchain transactions being tracked (append-only). We use a composite pk (txn_hash, public_key)
here because a single transaction hash can theoretically be associated to multiple addresses (e.g. 1 sender, n recipients)

| field           | type                | description                                                                                              |
|-----------------|---------------------|----------------------------------------------------------------------------------------------------------|
| txn_hash (pk)   | STRING MAX          | this transactions identifier hash
| public_key (pk) | STRING MAX          | the participating addresses (public keys) in this txn                                                    |
| txn_timestamp   | TIMESTAMP           | the time this transaction was verified on theblockchain                                                  |
| amount          | FLOAT64             | the value being transacted in USD                                                                        |
| fee             | FLOAT64             | the fee incurred for this transacton in USD                                                              |
| created_at      | TIMESTAMP           | the point in time this record was created (UTC)                                                          |
| tags            | STRING MAX          | a comma-delimited list of "tags" that categorize this transaction, e.g. "transfer"                       |

---

## API Design

The actions we want to implement are pretty much outlined in the instructions. As for implementation details, I'll work with more or less vanilla Golang packages `net/http` for standard library HTTP client/server things and `gorilla/mux` to leverage its handy routing functionality.

1. `func add(addr string)`: add adds a BTC wallet if it doesn't already exist and imports its associated transactions
2. `func balance(addr string) float64`: balance gets the current balance of the give BTC address (note: the returned value can be out of date, if we want the most up-to-date balance, we have to call `sync` first)
3. `func transactions(addr string) []*Transaction`: transactions gets the current transactions associated with the provided BTC address
4. `func sync(addr string)`: sync fetches the latest address data from the BTC blockchain and synchronizes the relevant tables accordingly
5. `func detectTransfers`: detectTransfers detects the likely transfers between a user's wallets with fuzzy matching based on transaction amounts and corresponding timestamps (+- a few mins)

## Questions

- What challenges do you anticipate building and running this system?

This system in particular requires a nuanced understanding of the API and structure of the blockchain data being fetched. Blockchain data is constantly updated globally, so the likelihood of stale data is high (requiring continous syncing of some sort in the real world) and a tradeoff is when shaping that blockchain data into database schemas (pros: transactionalized reads/writes, possible to leverage relationships between tables; cons: using rigid schemas require maintenance and/or migrations if doing anything beyond adding a new column, syncing can take a long time and the API used a bottleneck, very intentional thinking around the UX is probably necessary in order to know how these tradeoffs should land).

Ideally (or at least in a less time-boxed cirumstances), we would be optimize sync to be parallelizable (using goroutines, some sort of job infrastructure or leveraging a message broker like Pub/Sub)

- How will you test your system?

Unit tests for isolated, one-off functions like helpers, batching logic, etc. Go table-driven testing and/or golden files could prove particularly useful to implement solid test coverage here.

Integration tests particularly between our database (Spanner in this case) and our blockchain data API

- How will you monitor system health in production?

First, I'd need to update the logs to be more informative (include context, expected data, the request itself, etc.) to establish some baseline visibility. If a stack similar to the one used here, we could use/export logs in GCP including request traces and database query speeds.

From there, we could build log-based alerts per API endpoint that'd trigger on expected errors as well as a general health-check ping that could regularly check that the service is returning OK responses where we expect it to.

Lots of how these would be communicated are then team dependent -- maybe these alerts hook into particular slack channels for the team to respond to, or generate a bug ticket, etc.

## Usage

Here are a few example commands used to demonstrate the working server (and its output):

```bash
# in a separate tab
➜  cointracker-eng-assignment git:(main) ✗ go run main.go
2022/01/05 14:15:40 Listening on port 8080...

# adding a new BTC wallet
➜  cointracker-eng-assignment git:(main) curl -X POST http://localhost:8080/add -H "Content-Type: application/json" -d @test_json/happy_path.json | jq
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   304  100   251  100    53   1426    301 --:--:-- --:--:-- --:--:--  1727
{
  "address": {
    "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
    "Balance": 686.54901305,
    "CreatedAt": "2022-01-05T05:32:09.740009Z",
    "UpdatedAt": "2022-01-05T05:32:09.740009Z",
    "LastTxnHash": "f72b90635567150f41840426e522989ebf00718aa30f6ffb4bd766d968af88bb"
  }
}

# getting a wallet's balance
➜  cointracker-eng-assignment git:(main) ✗ curl -X POST http://localhost:8080/balance -H "Content-Type: application/json" -d @test_json/happy_path.json | jq
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100    77  100    24  100    53      7     16  0:00:03  0:00:03 --:--:--    24
{
  "balance": 730.7941962
}

# getting all transactions (only 5 shown here)
{
  "transactions": [
    {
      "TxnHash": "",
      "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
      "Amount": 832564,
      "Fee": 7.31006,
      "Tags": "",
      "TxnTimestamp": "2021-11-09T07:56:15Z",
      "CreatedAt": "0001-01-01T00:00:00Z"
    },
    {
      "TxnHash": "",
      "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
      "Amount": 68898.8,
      "Fee": 20.8019,
      "Tags": "",
      "TxnTimestamp": "2021-12-29T17:55:43Z",
      "CreatedAt": "0001-01-01T00:00:00Z"
    },
    {
      "TxnHash": "",
      "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
      "Amount": 541962,
      "Fee": 15.6571,
      "Tags": "",
      "TxnTimestamp": "2021-12-30T11:30:00Z",
      "CreatedAt": "0001-01-01T00:00:00Z"
    },
    {
      "TxnHash": "",
      "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
      "Amount": 30198.8,
      "Fee": 1.17351,
      "Tags": "",
      "TxnTimestamp": "2021-12-08T07:59:29Z",
      "CreatedAt": "0001-01-01T00:00:00Z"
    },
    {
      "TxnHash": "",
      "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
      "Amount": 38987.9,
      "Fee": 20.0698,
      "Tags": "",
      "TxnTimestamp": "2021-11-20T16:05:53Z",
      "CreatedAt": "0001-01-01T00:00:00Z"
    }
}

# manually syncing the wallet
➜  cointracker-eng-assignment git:(main) ✗ curl -X POST http://localhost:8080/sync -H "Content-Type: application/json" -d @test_json/happy_path.j
son | jq
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   315  100   262  100    53    713    144 --:--:-- --:--:-- --:--:--   858
{
  "address": {
    "PublicKey": "3E8ociqZa9mZUSwGdSmAEMAoAxBK3FNDcd",
    "Balance": 730.7941962,
    "CreatedAt": "2022-01-05T14:26:01.7219867-05:00",
    "UpdatedAt": "2022-01-05T14:26:01.7219867-05:00",
    "LastTxnHash": "ac0d773d7b0a6eb650eff8a40dd5be4b4a370bf28aa30767d783ba4b5929d6f6"
  }
}

# detect likely transfers
➜  cointracker-eng-assignment git:(main) ✗ curl -X POST http://localhost:8080/detect-transfer -H "Content-Type: application/json" -d @test_json/detect_transfers_multiple_possible_transfers.json | jq
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100  1326  100    42  100  1284  42000  1253k --:--:-- --:--:-- --:--:-- 1294k
{
  "tx_id_1": "tx_id_3",
  "tx_id_5": "tx_id_4"
}

```
