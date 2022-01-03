package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"cloud.google.com/go/spanner"
	"github.com/gorilla/mux"
	"google.golang.org/api/option"
)

// Server represents a basic web server backed by a Google Spanner as a data store
type Server struct {
	context context.Context
	router  *mux.Router
	spanner *spanner.Client
}

const (
	address = "localhost"
	port    = "8080"

	// todo: replace with better names & use environment variables in the real world
	projectID  = "cointracker-test-1234"
	instanceID = "test-instance"
	databaseID = "test-db"
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

	r := mux.NewRouter()

	// todo: rename routes to better reflect specific functionality (i.e. we're only handling btc addresses)
	r.Handle("/add", AddHandler())

	return &Server{
		context: ctx,
		router:  r,
		spanner: spannerClient,
	}
}

// AddHandler returns a closure responsible for validating the incoming request
// and invoking add() to create a new BTC address
func AddHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// todo: validate request
	})
}

// add adds a BTC wallet if it doesn't already exist and imports its associated transactions
func add(string addr) {
	// todo: check if this address already exists in the addresses table

	// todo: write this new address if DNE

	// todo: automatically sync transactions relevant to this address
}

func main() {
	server := InitServer()

	log.Printf("Listening on port %s...\n", port)
	log.Fatal(http.ListenAndServe(
		fmt.Sprintf("%s:%s", address, port), server.router),
	)
}
