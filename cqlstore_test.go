package cqlstore_test

import (
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

func TestMySuite(t *testing.T) {
	suite.Run(t, new(testSuite))
}

// SetupTest runs before each test to make a new keyspace for testing.
func (suite *testSuite) SetupTest() {
	url := os.Getenv("CQLSTORE_URL")
	keyspace := os.Getenv("CQLSTORE_KEYSPACE")
	if url == "" || keyspace == "" {
		msg := "Missing required environment vars. Tests requires a running " +
			"Cassandra instance with the environment vars CQLSTORE_URL and " +
			"CQLSTORE_KEYSPACE defined"
		suite.Fail(msg)
	}

	suite.cluster = gocql.NewCluster(url)

	sess, err := suite.cluster.CreateSession()
	suite.NoError(err)

	create := fmt.Sprintf(`
		CREATE KEYSPACE %q WITH REPLICATION = {
			'class' : 'SimpleStrategy',
			'replication_factor' : 1
		}`,
		keyspace,
	)
	err = sess.Query(create, suite.cluster.Keyspace).Exec()
	suite.NoError(err)

	suite.cluster.Keyspace = keyspace
}

// TearDownTest runs after each test to drop the keyspace we create for this test
func (suite *testSuite) TearDownTest() {
	sess, err := suite.cluster.CreateSession()
	suite.NoError(err)

	err = sess.Query(fmt.Sprintf("DROP KEYSPACE %q", suite.cluster.Keyspace)).Exec()
	suite.NoError(err)
}

func (suite *testSuite) TestSessionData() {
	store, err := cqlstore.New(suite.cluster, "sessions", []byte("foo-bar-baz"))
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
