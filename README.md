# cqlstore

A Cassandra implementation for `github.com/gorilla/sessions`

Example and API references on GoDoc [![GoDoc][godoc-badge]][godoc]

# Stability Note

This package has not yet been tested in a production environment. I do not
expect any breaking changes or significant performance issues but until it has
been tested in the wild this notice will remain.

# Testing

Tests require an active Cassandra DB. You must use environment variables to
specify the Cassandra URL and the name of a test keyspace. This keyspace will
be created and dropped as part of the test suite.

    CQLSTORE_KEYSPACE=foobar CQLSTORE_URL=dockerhost go test

[godoc]: https://godoc.org/github.com/jcbwlkr/cqlstore "GoDoc"
[godoc-badge]: https://godoc.org/github.com/jcbwlkr/cqlstore?status.svg "GoDoc Badge"
