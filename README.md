# CoinTracker Engineering Assignment

Hey CoinTracker team! My name is Jeff, and thanks for taking the time to reading my technical assessment -- I'm geniunely looking forward to working on this take-home (and of course hope you all like what you see haha).

In case you don't already have it handy, the original instructions are [here](https://cointracker.notion.site/CoinTracker-Engineering-Assignment-ac869846380545dbb1c8ad4f947a8e29)

---

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

The `users` table would be responsible for storing a naive implementation of a user for this web app. TBD transfer functionality in which a user can have multiple addresses.

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

1. Unit tests for isolated, one-off functions like helpers, batching logic, etc. Go table-driven testing could prove particularly useful here (testing the different types of payloads each handler can receive, etc.)
2. Integration tests particularly between our database (Spanner in this case) and our blockchain data API

- How will you monitor system health in production?

First, I'd need to update the logs to be more informative (include context, expected data, the request itself, etc.) to establish some baseline visibility. If a stack similar to the one used here, we could use/export logs in GCP including request traces and database query speeds.

From there, we could build log-based alerts per API endpoint that'd trigger on expected errors as well as a general health-check ping that could regularly check that the service is returning OK responses where we expect it to.

Lots of how these would be communicated are then team dependent -- maybe these alerts hook into particular slack channels for the team to respond to, or generate a bug ticket, etc.