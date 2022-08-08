package chainparse

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

type ChainParser struct {
	fetcher *fetcher
}

func NewChainParser(rt http.RoundTripper) *ChainParser {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &ChainParser{fetcher: &fetcher{
		rt: rt,
	}}
}

func RetrieveChainData(ctx context.Context, rt http.RoundTripper) ([]*ChainSchema, error) {
	fetcher := &fetcher{rt: rt}
	return fetcher.fetchChainData(ctx)
}

func (cp *ChainParser) FetchData(rw http.ResponseWriter, req *http.Request) {
	ctx, span := trace.StartSpan(req.Context(), "FetchData")
	defer span.End()

	// 1. Fetch the various values.
	chainSchemaL, err := cp.fetcher.fetchChainData(ctx)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to retrieve all chain schema")
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(rw)
	if err := enc.Encode(chainSchemaL); err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to JSON marshal & send the retrieved chain info")
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
}
