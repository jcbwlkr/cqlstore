package cqlstore_test

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gocql/gocql"
	"github.com/jcbwlkr/cqlstore"
)

// This example shows connecting to a Cassandra cluster, initalizing the
// CQLStore, then running a simple HTTP server that keeps track of the number
// of times the user has hit the page.
func Example_basic() {
	// Connect to your Cassandra cluster
	cluster := gocql.NewCluster("192.168.59.103")
	cluster.Keyspace = "demo"
	dbSess, err := cluster.CreateSession()
	if err != nil {
		log.Fatalln(err)
	}
	defer dbSess.Close()

	// Create the CQLStore
	store, err := cqlstore.New(dbSess, "sessions", []byte("something-secret"))
	if err != nil {
		log.Fatalln(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Don't bump counter for things like /favicon.ico
		if r.URL.String() != "/" {
			return
		}

		// Get session named "demo-session" identified by the request's cookies
		session, err := store.Get(r, "demo-session")
		if err != nil {
			// Error loading existing session. Session might have expired,
			// their cookie was malformed, or there was a database issue.
			log.Println(err)
		}

		counter, ok := session.Values["counter"].(int)
		if !ok {
			counter = 0
		}
		session.Values["counter"] = counter + 1

		// Save session values to DB and add header to response to set cookie
		err = session.Save(r, w)
		if err != nil {
			// Error saving session. Probably pretty important.
			log.Println(err)
		}

		fmt.Fprintf(w, "I have seen you %d time(s)\n", session.Values["counter"])
	})

	http.ListenAndServe(":8080", nil)
}
