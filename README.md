# chainparse
Chain registry parser and builder using the latest data from the Chain registry
to then build the latest status of chains in Cosmos ecosystem.

This tool generates a full on spreadsheet with the latest
status of the Cosmos registry registered chains.

## Using it
This code generates CSV printed to stdout, then errors to stderr.
To use it
```shell
rm -rf registry && go run main.go > listing.csv
```

then open `listing.csv` perhaps in your Excel-like software/analyzer.


## Why use Go?
The reason why we are using Go instead of say Javascript is because
traversing directories for the various chains would require
.zip parsing, unzipping, writing to directories,
traversing directories, then parsing go.mod files,
and then generating the sheet.

## Copyright
Copyright Cosmos ecosystem and authors.
