// Package cqlstore provides an Apache Cassandra implementation of HTTP session
// storage for github.com/gorilla/sessions.
package cqlstore

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/gocql/gocql"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// CQLStore provides a Cassandra backed implementation of the interface Store
// from github.com/gorilla/sessions
type CQLStore struct {
	Options *sessions.Options
	Codecs  []securecookie.Codec

	saveQ   *gocql.Query
	deleteQ *gocql.Query
	loadQ   *gocql.Query
}

// New creates a new CQLStore. It requires an active gocql.Session and the name
// of the table where it should store session data. It will create this table
// with the appropriate schema if it does not exist. Additionally pass one or
// more byte slices to serve as authentication and/or encryption keys for both
// the cookie's session ID value and the values stored in the database.
func New(cs *gocql.Session, table string, keypairs ...[]byte) (*CQLStore, error) {
	var err error

	re := regexp.MustCompile("^[a-zA-Z0-9_]+$")
	if !re.MatchString(table) {
		return &CQLStore{}, errors.New("Invalid table name " + table)
	}

	// TODO add more columns for timestamps?
	create := `
	CREATE TABLE IF NOT EXISTS "` + table + `" (
		id uuid,
		data text,
		PRIMARY KEY (id)
	)`
	err = cs.Query(create, table).Exec()
	if err != nil {
		return &CQLStore{}, createError{err}
	}

	st := &CQLStore{
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: 86400 * 30,
		},
		Codecs: securecookie.CodecsFromPairs(keypairs...),

		saveQ:   cs.Query(`INSERT INTO "` + table + `" ("id", "data") VALUES(?, ?) USING TTL ?`),
		deleteQ: cs.Query(`DELETE FROM "` + table + `" WHERE "id" = ?`),
		loadQ:   cs.Query(`SELECT "data" FROM "` + table + `" WHERE "id" = ?`),
	}

	return st, nil
}

// Get creates or returns a session from the request registry. It never returns
// a nil session.
func (st *CQLStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(st, name)
}

// New creates and returns a new session without adding it to the registry. If
// the request has the named cookie then it will decode the session ID and load
// session values from the database. If the request might already have had the
// session loaded then calling Get instead will be faster. It never returns a
// nil session.
func (st *CQLStore) New(r *http.Request, name string) (*sessions.Session, error) {
	s := sessions.NewSession(st, name)
	s.Options = &(*st.Options)
	s.IsNew = true

	// See if the request has a cookie for this session. If it does not we can
	// just return the new session struct.
	c, errCookie := r.Cookie(name)
	if errCookie != nil {
		return s, nil
	}

	// Okay so the request identified a session. Try to load it.

	// Decode the cookie value into the session id
	if err := securecookie.DecodeMulti(name, c.Value, &s.ID, st.Codecs...); err != nil {
		return s, loadError{err}
	}

	var encData string
	if err := st.loadQ.Bind(s.ID).Scan(&encData); err != nil {
		return s, loadError{err}
	}

	if err := securecookie.DecodeMulti(s.Name(), encData, &s.Values, st.Codecs...); err != nil {
		return s, loadError{err}
	}

	s.IsNew = false

	return s, nil
}

// Save persists session values to the database and adds the session ID cookie
// to the request. Save must be called before writing the response or the
// cookie will not be sent.
func (st *CQLStore) Save(r *http.Request, w http.ResponseWriter, s *sessions.Session) error {
	if s.Options.MaxAge < 0 {
		if err := st.deleteQ.Bind(s.ID).Exec(); err != nil {
			return saveError{err}
		}

		http.SetCookie(w, sessions.NewCookie(s.Name(), "", s.Options))
		return nil
	}

	if s.ID == "" {
		// TODO is there a better one to use here?
		s.ID = gocql.UUIDFromTime(time.Now()).String()
	}

	// Encode the data to store in the db
	encData, err := securecookie.EncodeMulti(s.Name(), s.Values, st.Codecs...)
	if err != nil {
		return saveError{err}
	}

	if err := st.saveQ.Bind(s.ID, encData, st.Options.MaxAge).Exec(); err != nil {
		return saveError{err}
	}

	// Encode the session ID and set it in a cookie
	encID, err := securecookie.EncodeMulti(s.Name(), s.ID, st.Codecs...)
	if err != nil {
		return saveError{err}
	}
	http.SetCookie(w, sessions.NewCookie(s.Name(), encID, s.Options))

	return nil
}

// TODO better error handling

type createError struct {
	err error
}

func (e createError) Error() string {
	return "Could not create sessions table. Error: " + e.err.Error()
}

type saveError struct {
	err error
}

func (e saveError) Error() string {
	return "Could not save session data. Error: " + e.err.Error()
}

type loadError struct {
	err error
}

func (e loadError) Error() string {
	return "Could not load session data. Error: " + e.err.Error()
}
