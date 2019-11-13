# lotrproxypdf - Create LOTR LCG proxy cards in a PDF

[![Build Status](https://travis-ci.org/xdg-go/lotrproxypdf.svg?branch=master)](https://travis-ci.org/xdg-go/lotrproxypdf) [![Go Report Card](https://goreportcard.com/badge/github.com/xdg-go/lotrproxypdf)](https://goreportcard.com/report/github.com/xdg-go/lotrproxypdf) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

A command line tool in Go to create proxy cards for the Lord of the Rings LCG.

See the article, [Go as glue: JSON + XML + PNG =
PDF](https://xdg.me/blog/go-as-glue-json-xml-png-pdf/), for motivation and a
description of the implementation.

# Installation

`lotrproxypdf` requires Go 1.13 or later.  From outside any existing Go
project, run this command:

```
go get github.com/xdg-go/lotrproxypdf
```

# Usage

`lotrproxypdf` takes two command line arguments.  The first is the file name
of an OCTGN deck list file (typically downloaded from
[RingsDB](https://ringsdb.com/decklists).  The second is the file name where
the PDF file will be written:

```
lotrproxypdf mydeck.o8d mydeck.pdf
```

# Copyright and License

Copyright 2019 by David A. Golden. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License").
You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0
