# GoTinyDB

[![GoDoc](https://godoc.org/github.com/alexandrestein/gotinydb?status.svg)](https://godoc.org/github.com/alexandrestein/gotinydb)
[![Build Status](https://api.cirrus-ci.com/github/alexandrestein/gotinydb.svg)](https://cirrus-ci.com/github/alexandrestein/gotinydb)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Falexandrestein%2Fgotinydb.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Falexandrestein%2Fgotinydb?ref=badge_shield)
[![codecov](https://codecov.io/gh/alexandreStein/GoTinyDB/branch/master/graph/badge.svg)](https://codecov.io/gh/alexandreStein/GoTinyDB) 
[![Go Report Card](https://goreportcard.com/badge/github.com/alexandrestein/gotinydb)](https://goreportcard.com/report/github.com/alexandrestein/gotinydb)
[![License](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)
[![codebeat badge](https://codebeat.co/badges/f00fff9a-bfb8-429e-b285-cef39a0e4fd4)](https://codebeat.co/projects/github-com-alexandrestein-gotinydb-master)

The goal is to have a fairly simple database which is light and don't needs to fit in RAM. This part is done by [Badger](https://github.com/dgraph-io/badger) which use SSD capabilities to have low
RAM consumption and good performances.

The indexing is done by [Bleve](https://blevesearch.com).

It's pure Go language, so no issues with CGO.

## Features

### Collections

Save deferent document type in different collection.

The database can have many collections, [see prefix limitations](#prefixes).
many collection can be used on the same database.

### Index and query is done by [Bleve](https://blevesearch.com)

It's a fully featured indexing package.
Indexing is done at the collection level and one collection can have many indexes. [See prefix limitations](#prefixes).

### Files and media content

In the same database you can save files of any size and many small documents.

Supports for big content with io.Reader and io.Writer interface.
It split content into chunks of 5MB.

### Confidentiality and data integrity (encryption)

The all database content is encrypted and signed with [XChaCha20-Poly1305](https://godoc.org/golang.org/x/crypto/chacha20poly1305#NewX).

[See encryption limitations](#encryption)

## Installing

```bash
go get -u github.com/alexandrestein/gotinydb
```

## Getting started

The package is supposed to be used inside your software and at this point it is not supposed to be a dedicated database service.
Take a look at [GoDoc](https://godoc.org/github.com/alexandrestein/gotinydb) and to the examples folder.

## Road Map

- Make a basic master/slaves replication system for data protection

## Contributing

Any contribution will be appreciate.
Feedbacks and suggestions are welcome.

## Vendoring

We moved to go module.
In future Go release this will be the default vendoring behavior. So this will be transparent in the next release.

## Versioning

Version `v1.0.0` is not ready.
It's under development and versions have the form of v0.x.x.

There is no compatibility promise for now.

## Limitations

### Concurrency

Most of the methods can be run concurrently. But management actions can not:
- *DB.Backup
- *DB.Close
- *DB.DeleteCollection
- *DB.Load
- *Collection.DeleteIndex
- *Collection.SetBleveIndex

Those functions and methods should be run on idle database.

### Prefixes

Prefixes to split different parts of the database: collection, files, indexes and documents.
The prefixes are only 2 bytes (16bits). It gives 65536 possibilities which much more than enough.
But the hash collision are possible so the real limit is much less.

If you are unlucky you will hit this `ErrHashCollision`.

### Encryption

The database keys are not encrypted. But for indexing some of the content are used as keys.
For iteration reason the keys can't be encrypted.

For the content which needs to be sealed don't index them.
Bleve index mapping provides a very sine control of what are or not indexed.

## Author

- **Alexandre Stein** - [GitHub](https://github.com/alexandrestein)

## License

This project is licensed under the "Apache License, Version 2.0" see the [LICENSE](LICENSE) file for details or follow this [link](http://www.apache.org/licenses/LICENSE-2.0).

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Falexandrestein%2Fgotinydb.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Falexandrestein%2Fgotinydb?ref=badge_large)
