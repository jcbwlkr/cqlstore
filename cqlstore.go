package cqlstore

import (
	"net/http"
	"time"

	"github.com/gocql/gocql"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

type CQLStore struct {
	Options *sessions.Options
	Codecs  []securecookie.Codec

	table string
	cs    *gocql.Session
}

// TODO add expiration
// TODO method to close db
// TODO documentation
// TODO better error handling

func New(cluster *gocql.ClusterConfig, table string, keypairs ...[]byte) (*CQLStore, error) {
	// TODO sanitize table
	cs, err := cluster.CreateSession()
	_ = cs
	if err != nil {
		return &CQLStore{}, err
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

	// TODO prepare all of the queries I need and just stick them on my store

	st := &CQLStore{
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: 86400 * 30,
		},
		Codecs: securecookie.CodecsFromPairs(keypairs...),

		table: table,
		cs:    cs,
	}

	return st, nil
}

// Get creates or returns a session from the request registry. It never returns
// a nil session.
func (st *CQLStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(st, name)
}

// New creates and returns a new session without adding it to the registry. It
// never returns a nil session.
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

	sel := `SELECT "data" FROM "` + st.table + `" WHERE "id" = ?`
	var encData string
	if err := st.cs.Query(sel, s.ID).Scan(&encData); err != nil {
		return s, loadError{err}
	}

	if err := securecookie.DecodeMulti(s.Name(), encData, &s.Values, st.Codecs...); err != nil {
		return s, loadError{err}
	}

	s.IsNew = false

	return s, nil
}

// Save persists session to the database.
func (st *CQLStore) Save(r *http.Request, w http.ResponseWriter, s *sessions.Session) error {
	if s.ID == "" {
		// TODO is there a better one to use here?
		s.ID = gocql.UUIDFromTime(time.Now()).String()
	}

	// Encode the data to store in the db
	encData, err := securecookie.EncodeMulti(s.Name(), s.Values, st.Codecs...)
	if err != nil {
		return saveError{err}
	}

	// TODO add TTL
	insert := `INSERT INTO "` + st.table + `" ("id", "data") VALUES(?, ?) USING TTL ?`
	if err := st.cs.Query(insert, s.ID, encData, st.Options.MaxAge).Exec(); err != nil {
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
