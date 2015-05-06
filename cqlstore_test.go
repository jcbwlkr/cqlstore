package cqlstore_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gocql/gocql"
	"github.com/jcbwlkr/cqlstore"
	"github.com/stretchr/testify/suite"
)

type testSuite struct {
	suite.Suite
	cluster *gocql.ClusterConfig
}

// TestMySuite bootstraps and runs the test suite
func TestMySuite(t *testing.T) {
	suite.Run(t, new(testSuite))
}

// SetupTest runs before each test to make a new keyspace for testing.
func (suite *testSuite) SetupTest() {
	var err error
	suite.cluster, err = doSetup()
	suite.NoError(err)
}

// doSetup does the actual work of setting up. This is extracted so we can use
// it in benchmarks which don't have access to the suite helpers.
func doSetup() (*gocql.ClusterConfig, error) {
	url := os.Getenv("CQLSTORE_URL")
	keyspace := os.Getenv("CQLSTORE_KEYSPACE")
	if url == "" || keyspace == "" {
		msg := "Missing required environment vars. Tests requires a running " +
			"Cassandra instance with the environment vars CQLSTORE_URL and " +
			"CQLSTORE_KEYSPACE defined"
		return nil, errors.New(msg)
	}

	cluster := gocql.NewCluster(url)

	sess, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}

	create := fmt.Sprintf(`
		CREATE KEYSPACE %q WITH REPLICATION = {
			'class' : 'SimpleStrategy',
			'replication_factor' : 1
		}`,
		keyspace,
	)
	if err := sess.Query(create, cluster.Keyspace).Exec(); err != nil {
		return nil, err
	}

	cluster.Keyspace = keyspace

	return cluster, nil
}

// TearDownTest runs after each test to drop the keyspace we create for this test
func (suite *testSuite) TearDownTest() {
	err := doTearDown(suite.cluster)
	suite.NoError(err)
}

// doTearDown does the actual work of tearing down. This is extracted out so we
// can use it during benchmarking when the full suite isn't available.
func doTearDown(cluster *gocql.ClusterConfig) error {
	sess, err := cluster.CreateSession()
	if err != nil {
		return err
	}
	err = sess.Query(fmt.Sprintf("DROP KEYSPACE %q", cluster.Keyspace)).Exec()
	if err != nil {
		return err
	}

	return nil
}

func (suite *testSuite) TestSessionData() {
	dbSess, _ := suite.cluster.CreateSession()
	defer dbSess.Close()

	store, err := cqlstore.New(dbSess, "sessions", []byte("foo-bar-baz"))
	suite.NoError(err)

	// Step 1 ------------------------------------------------------------------
	// Make a request, set some values, and save the session. Check that the
	// session id cookie is set and nothing errors out.
	req1, err := http.NewRequest("GET", "http://www.example.com/", nil)
	suite.NoError(err)

	sess, err := store.Get(req1, "test-sess")
	suite.NotNil(sess)
	suite.NoError(err)
	suite.True(sess.IsNew)

	sess.Values["foo"] = "Foo"
	sess.Values["bar"] = "Bar"

	w := httptest.NewRecorder()
	err = sess.Save(req1, w)
	suite.NoError(err)

	if _, ok := w.Header()["Set-Cookie"]; !ok {
		suite.Fail("Missing expected header Set-Cookie")
	}

	// Step 2 ------------------------------------------------------------------
	// Make a new request using the same cookies set from the initial request
	// then check that the previously saved session values are still there.
	req2, err := http.NewRequest("GET", "http://www.example.com/", nil)
	suite.NoError(err)
	// Filter the test recorder through a Response so we can get cookie values
	resp := http.Response{Header: w.Header()}
	for _, c := range resp.Cookies() {
		req2.AddCookie(c)
	}

	sess2, err := store.Get(req2, "test-sess")
	suite.NotNil(sess2)
	suite.NoError(err)
	suite.False(sess2.IsNew)
	suite.Equal(sess2.Values["foo"], "Foo")
	suite.Equal(sess2.Values["bar"], "Bar")

	// Step 3 ------------------------------------------------------------------
	// Make a third request without cookies and make sure it gets blank values
	req3, err := http.NewRequest("GET", "http://www.example.com/", nil)
	suite.NoError(err)

	sess3, err := store.Get(req3, "test-sess")
	suite.NotNil(sess3)
	suite.NoError(err)
	suite.True(sess3.IsNew)
	suite.Empty(sess3.Values)

	// Step 4 ------------------------------------------------------------------
	// Make a fourth request without bogus cookies and make sure it errors
	req4, err := http.NewRequest("GET", "http://www.example.com/", nil)
	req4.AddCookie(&http.Cookie{Name: "test-sess", Value: "bogus"})
	_, err = store.Get(req4, "test-sess")
	suite.Error(err)
}

func (suite *testSuite) TestDeletingASession() {
	dbSess, _ := suite.cluster.CreateSession()
	defer dbSess.Close()

	store, err := cqlstore.New(dbSess, "sessions", []byte("foo-bar-baz"))
	suite.NoError(err)

	// Step 1 ------------------------------------------------------------------
	// Make a session with some data. Check that it's in the store.
	req1, err := http.NewRequest("GET", "http://www.example.com/", nil)
	suite.NoError(err)

	sess, err := store.Get(req1, "test-sess")
	suite.NotNil(sess)
	suite.NoError(err)
	suite.True(sess.IsNew)

	sess.Values["jerry"] = "Seinfeld"

	w := httptest.NewRecorder()
	err = sess.Save(req1, w)
	suite.NoError(err)

	var count int
	err = dbSess.Query(`SELECT count(*) FROM "sessions"`).Scan(&count)
	suite.NoError(err)
	suite.Equal(1, count)

	// Step 2 ------------------------------------------------------------------
	// Delete the session by setting the maxage to -1. Ensure the cookie is set
	// to expire and it's removed from the store.
	sess.Options.MaxAge = -1

	w2 := httptest.NewRecorder()
	err = sess.Save(req1, w2)
	suite.NoError(err)

	// Ensure our response tells the browser to clear their value for the
	// session id cookie
	cookieHeader, ok := w2.Header()["Set-Cookie"]
	if !ok {
		suite.Fail("Missing expected header Set-Cookie")
	}
	suite.Equal("test-sess=; ", cookieHeader[0][:12])

	// Ensure it was deleted from the db too
	err = dbSess.Query(`SELECT count(*) FROM "sessions"`).Scan(&count)
	suite.NoError(err)
	suite.Equal(0, count)
}

func (suite *testSuite) TestBadTableNames() {
	dbSess, _ := suite.cluster.CreateSession()
	defer dbSess.Close()

	_, err := cqlstore.New(dbSess, `1"; DROP TABLE students; --`, []byte("bobby-tables"))
	suite.Error(err)
}

func (suite *testSuite) TestSettingOptionsOnOneDoesNotSetForAll() {
	dbSess, _ := suite.cluster.CreateSession()
	defer dbSess.Close()

	store, err := cqlstore.New(dbSess, `sessions`, []byte("banana"))
	suite.NoError(err)

	store.Options.MaxAge = 1800

	r, err := http.NewRequest("GET", "http://www.example.com/", nil)
	suite.NoError(err)

	sess, err := store.Get(r, "test-sess")
	suite.NoError(err)

	sess.Options.MaxAge = 900

	w := httptest.NewRecorder()
	sess.Save(r, w)

	// Setting the MaxAge value above should not affect the store (and
	// therefore all subsequently generated sessions)
	suite.Equal(1800, store.Options.MaxAge)
}

// BenchmarkARoundTrip measures the time it takes to make a new session, save
// it with some values, then make a new request that loads the same session.
func BenchmarkARoundTrip(b *testing.B) {
	cluster, err := doSetup()
	if err != nil {
		b.Error(err)
	}
	dbSess, err := cluster.CreateSession()
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store, _ := cqlstore.New(dbSess, "sessions", []byte("bench-me"))
		req1, _ := http.NewRequest("GET", "http://www.example.com/", nil)
		sess, _ := store.Get(req1, "test-sess")

		sess.Values["foo"] = "Foo"
		sess.Values["bar"] = "Bar"

		w := httptest.NewRecorder()
		_ = sess.Save(req1, w)

		req2, _ := http.NewRequest("GET", "http://www.example.com/", nil)

		// Filter the test recorder through a Response so we can get cookie values
		resp := http.Response{Header: w.Header()}
		for _, c := range resp.Cookies() {
			req2.AddCookie(c)
		}

		_, _ = store.Get(req2, "test-sess")

		// I want each pass to make the table but I don't want to count the
		// time I spend dropping it between passes
		b.StopTimer()
		_ = dbSess.Query(`DROP TABLE "sessions"`).Exec()
		b.StartTimer()
	}
	b.StopTimer()

	err = doTearDown(cluster)
	if err != nil {
		b.Error(err)
	}
}
