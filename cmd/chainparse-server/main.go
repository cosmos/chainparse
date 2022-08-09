package main

import (
	_ "embed"
	"flag"
	"net/http"

	"contrib.go.opencensus.io/exporter/ocagent"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"

	"github.com/cosmos/chainparse"
)

//go:embed mock.json
var mockDataJSON []byte

func main() {
	ocAgentAddress := flag.String("ocagent-addr", "", "The address to connect to the OCAgent")

	addr := flag.String("addr", ":8834", "The address to serve traffic on")
	flag.Parse()

	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
		ocagent.WithServiceName("cmd/chainparse"),
		ocagent.WithAddress(*ocAgentAddress),
	)
	if err != nil {
		panic(err)
	}

	if len(mockDataJSON) == 0 {
		panic("mockDataJSON is empty!")
	}

	mux := http.NewServeMux()
	cp := chainparse.NewChainParser(new(ochttp.Transport))
	mux.HandleFunc("/", http.HandlerFunc(cp.FetchData))
	mux.HandleFunc("/mock", http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write(mockDataJSON)
	}))

	logrus.WithFields(logrus.Fields{
		"addr": *addr,
	}).Info("Serving traffic")

	// Set the tracer.
	trace.ApplyConfig(trace.Config{
		DefaultSampler: trace.AlwaysSample(),
	})
	trace.RegisterExporter(oce)
	ocmux := &ochttp.Handler{
		Handler: mux,
	}

	if err := http.ListenAndServe(*addr, ocmux); err != nil {
		panic(err)
	}
}
