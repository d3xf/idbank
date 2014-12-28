# idbank

[![Godoc](http://img.shields.io/badge/godoc-reference-blue.svg?style=flat)](https://godoc.org/github.com/d3xf/idbank) [![license](http://img.shields.io/badge/license-MIT-red.svg?style=flat)](https://raw.githubusercontent.com/d3xf/idbank/master/LICENSE)

The idbank package provides a concurrency-safe service for allocating
unique temporary identifiers.

In environments with many concurrently-executing services or processes, it
is sometimes desirable to dispense globally unique identifiers on a
provisional basis.  An identifier is reclaimed either when explicitly
released by a client, or after an expiration period.  A client may reset
the expiration time for an identifier to keep it from expiring while it's
still in use.

This package implements such a dispenser for identifiers of type
uint32. (Other identifier types can be supported with minimal changes.)
The basic abstraction is a Bank, representing a store of identifiers
within a user-specified range that supports allocation, release, and
expiration-reset operations.  When an identifier is allocated, a random
token is returned that must be supplied when attempting a reset or
release, preventing accidental disruption by other clients.  All
operations are safe for concurrent use.

A small example HTTP server program alongside the package provides a
simple JSON REST API for interacting with an identifier microservice.

```
$ curl -X PUT http://host:port/id/alloc?name=foo&timeout=300
{"id": 100, "client": "foo", "timeout": 300, "token": 247946913}

$ curl http://host:port/id/100
{"id": 100, "client": "foo"}

$ curl -X PUT http://host:port/id/reset?id=100&timeout=60&token=247946913
{"error": "success"}

$ curl -X PUT http://host:port/id/release?id=100&token=247946913
{"error": "success"}
```

## installation

```
go get github.com/d3xf/idbank
```

## documentation
See [Godoc](http://godoc.org/github.com/d3xf/idbank) for full
documentation.

