# CoinTracker Engineering Assignment

Original instructions [here](https://cointracker.notion.site/CoinTracker-Engineering-Assignment-ac869846380545dbb1c8ad4f947a8e29)

Hey CoinTracker team! My name is Jeff, and thanks for taking the time to reading my technical assessment -- I'm geniunely looking forward to working on this take-home (and of course hope you all like what you see haha).

---

## Goals

### Immediate Requirements
- Add bitcoin addresses
- View the current balance for a given bitcoin address
- View the historical transactions for a given bitcoin address
- Synchronize data to retrieve the latest balances and transactions on each bitcoin address
- Detect transfers between the user's addresses

### Future Goals ("Nice-to-haves")

These are some things that I'lll keep in mind (both as potential considerations for designing the system and for neat additions that weren't explicitly asked for). Not the focus here, but helpful to enumerate these nonetheless.

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

The `users` table would be responsible for storing a naive implementation of a user for this web app

| field     | type       | description                       |
|-----------|------------|-----------------------------------|
| uuid      | STRING MAX | unique identifier for this user   |
| username  | STRING MAX | human-readable name for this user |
| addresses | STRING MAX | comma-delimited list of addresses |

note: the MAX keyword specifies no "hard limit" for a given field, but is internally [optimized](https://stackoverflow.com/questions/45964937/performance-difference-for-stringmax) to store the limited length bytes

The `addresses` table stores the public key addresses added to the app

| field         | type       | description                                                 |
|---------------|------------|-------------------------------------------------------------|
| public_key    | STRING MAX | the public key of this address                              |
| balance       | FLOAT64    | the amount stored at this address                           |
| created_at    | TIMESTAMP  | the point in time this record was created (UTC)             |
| updated_at    | TIMESTAMP  | the point in time this record was last updated (UTC)        |
| last_txn_hash | STRING MAX | the most recent transaction hash associated to this address |

The `transactions` table is responsible for storing blockchain transactions being tracked (append-only)

| field      | type       | description                                                                                                       |
|------------|------------|-------------------------------------------------------------------------------------------------------------------|
| txn_hash   | STRING MAX | this transactions identifier hash                                                                                 |
| from_addr  | STRING MAX | the source address for this transaction                                                                           |
| to_addr    | STRING MAX | the destination address for this transaction                                                                      |
| amount     | FLOAT64    | the value being transacted                                                                                        |
| timestamp  | TIMESTAMP  | the time this transaction was verified on the blockchain                                                          |
| created_at | TIMESTAMP  | the point in time this record was created (UTC)                                                                   |
| tags       | STRING MAX | a comma-delimited list of "tags" that characterizes this transaction and is displayed to the user e.g. "transfer" |