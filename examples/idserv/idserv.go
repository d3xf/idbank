/*

A trivial HTTP/JSON API for the idbank service.

Examples:

    $ curl -X PUT http://host:port/id/alloc?name=foo&timeout=300
    {"id": 100, "client": "foo", "timeout": 300, "token": 247946913}

    $ curl http://host:port/id/100
    {"id": 100, "client": "foo"}

    $ curl -X PUT http://host:port/id/reset?id=100&timeout=60&token=247946913
    {"error": "success"}

    $ curl -X PUT http://host:port/id/release?id=100&token=247946913
    {"error": "success"}

*/
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/d3xf/idbank"
)

func validID(s string) (idbank.ID, bool) {
	if s == "" {
		return 0, false
	}
	id64, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return 0, false
	}
	return idbank.ID(id64), true
}
func validToken(s string) (int32, bool) {
	if s == "" {
		return 0, false
	}
	i64, err := strconv.ParseInt(s, 0, 32)
	if err != nil {
		return 0, false
	}
	return int32(i64), true
}
func validDuration(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	if d, err := strconv.Atoi(s); err != nil {
		return 0, false
	} else {
		return time.Duration(d) * time.Second, true
	}
}
func makeAllocHandler(B *idbank.Bank) http.Handler {
	f := func(w http.ResponseWriter, r *http.Request) {
		client := r.URL.Query().Get("name")
		exptime, ok := validDuration(r.URL.Query().Get("timeout"))
		if ok && len(client) > 0 && r.Method == "PUT" {
			id, token, err := B.Alloc(client, exptime)
			if err != nil {
				w.Write([]byte(`{"error": "` + err.Error() + `"}` + "\n"))
			} else {
				w.Write([]byte(fmt.Sprintf(`{"id": %v, "client": "%v", "timeout": %v, "token": %v}`+"\n",
					id, client, exptime.Seconds(), token)))
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	return http.HandlerFunc(f)
}
func makeQueryHandler(B *idbank.Bank) http.Handler {
	f := func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		id, ok := validID(p[strings.LastIndex(p, "/")+1:])
		if ok && r.Method == "GET" {
			client := B.Query(id)
			if client == "" {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.Write([]byte(fmt.Sprintf(`{"id": %v, "client": "%v"}`+"\n", id, client)))
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	return http.HandlerFunc(f)
}
func makeReleaseHandler(B *idbank.Bank) http.Handler {
	f := func(w http.ResponseWriter, r *http.Request) {
		id, ok_i := validID(r.URL.Query().Get("id"))
		token, ok_t := validToken(r.URL.Query().Get("token"))
		if ok_i && ok_t && r.Method == "PUT" {
			err := B.Release(id, token)
			if err != nil {
				w.WriteHeader(http.StatusNotAcceptable)
				w.Write([]byte(`{"error": "` + err.Error() + `"}` + "\n"))
			} else {
				// OK
				w.Write([]byte(`{"error": "success"}` + "\n"))
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	return http.HandlerFunc(f)
}
func makeResetHandler(B *idbank.Bank) http.Handler {
	f := func(w http.ResponseWriter, r *http.Request) {
		id, ok_i := validID(r.URL.Query().Get("id"))
		exptime, ok_e := validDuration(r.URL.Query().Get("timeout"))
		token, ok_t := validToken(r.URL.Query().Get("token"))
		if ok_i && ok_e && ok_t && r.Method == "PUT" {
			err := B.Reset(id, exptime, token)
			if err != nil {
				w.WriteHeader(http.StatusNotAcceptable)
				w.Write([]byte(`{"error": "` + err.Error() + `"}` + "\n"))
			} else {
				// OK
				w.Write([]byte(`{"error": "success"}` + "\n"))
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	return http.HandlerFunc(f)
}

func main() {
	log.SetPrefix("idserv ")

	if len(os.Args) < 2 {
		fmt.Printf("usage: %s [[host]:port]\n", os.Args[0])
		os.Exit(1)
	}
	addr := os.Args[1]

	B := idbank.New(0, 0)

	http.Handle("/id/alloc", makeAllocHandler(B))
	http.Handle("/id/reset", makeResetHandler(B))
	http.Handle("/id/release", makeReleaseHandler(B))
	http.Handle("/id/", makeQueryHandler(B))

	log.Println("Listening on", addr)
	err := http.ListenAndServe(addr, nil)
	log.Println(err)
	B.Destroy()
}
