all: darwin linux windows shasum

linux:
	GOOS=linux go build -o bin/chainparse_server_linux ./cmd/chainparse-server
darwin:
	GOOS=darwin go build -o bin/chainparse_server_darwin ./cmd/chainparse-server
windows:
	GOOS=windows go build -o bin/chainparse_server_windows ./cmd/chainparse-server

shasum:
	shasum -a 256 bin/*
