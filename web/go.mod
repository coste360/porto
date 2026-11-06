// This empty module keeps web/ (and its node_modules, some of which ship Go files) out of the
// github.com/coste360/porto module, so `go test ./...` and golangci-lint only see Porto's Go code.
module github.com/coste360/porto/web

go 1.24
