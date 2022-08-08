package chainparse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var testdataZip, testdataGoMod []byte

func init() {
	td, err := os.ReadFile("./testdata/registry/master.zip")
	if err != nil {
		panic(err)
	}
	testdataZip = td

	td, err = os.ReadFile("./testdata/registry/mod/go.mod")
	if err != nil {
		panic(err)
	}
	testdataGoMod = td
}

type alwaysToURLRoundTripper struct {
	destURL *url.URL
	next    *http.Client
}

func (art *alwaysToURLRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = art.destURL.Host
	req.URL.Scheme = art.destURL.Scheme
	return art.next.Do(req)
}

func TestFetchChainData(t *testing.T) {
	cst := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "go.mod") {
			rw.Write(testdataGoMod)
			return
		} else { // Otherwise they are requesting for the zip file
			rw.Write(testdataZip)
		}
	}))
	defer cst.Close()

	destURL, err := url.Parse(cst.URL)
	if err != nil {
		t.Fatal(err)
	}

	art := &alwaysToURLRoundTripper{next: cst.Client(), destURL: destURL}
	fetcher := &fetcher{rt: art}

	ctx := context.Background()
	got, err := fetcher.fetchChainData(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wantFirst3 := []*ChainSchema{
		{
			ChainName:    "agoric",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Agoric",
			Bech32Prefix: "agoric",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/Agoric/ag0/",
				RecommendedVersion: "agoric-3.1",
				CompatibleVersions: []string{"agoric-3.1"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "aioz",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "AIOZ Network",
			Bech32Prefix: "aioz",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/AIOZNetwork/go-aioz",
				RecommendedVersion: "v1.2.0",
				CompatibleVersions: []string{"v1.2.0"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "akash",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Akash",
			Bech32Prefix: "akash",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/ovrclk/akash/",
				RecommendedVersion: "v0.16.3",
				CompatibleVersions: []string{"v0.16.3"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
	}

	if diff := cmp.Diff(got[:3], wantFirst3); diff != "" {
		t.Fatalf("Mismatch: got - want +\n%s", diff)
	}

	wantLast3 := []*ChainSchema{
		{
			ChainName:    "thorchain",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "THORChain",
			Bech32Prefix: "thor",
			Codebase: &Codebase{
				GitRepoURL:         "https://gitlab.com/thorchain/thornode",
				RecommendedVersion: "chaosnet-multichain",
				CompatibleVersions: []string{"chaosnet-multichain"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "umee",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "umee",
			Bech32Prefix: "umee",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/umee-network/umee",
				RecommendedVersion: "v1.0.3",
				CompatibleVersions: []string{"v1.0.3"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "vidulum",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Vidulum",
			Bech32Prefix: "vdl",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/vidulum/mainnet",
				RecommendedVersion: "v1.0.0",
				CompatibleVersions: []string{"v1.0.0"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13",
			CosmosSDKVersion:  "v0.44.1",
			IBCVersion:        "v1.2.0",
		},
	}

	if diff := cmp.Diff(got[len(got)-3:], wantLast3); diff != "" {
		t.Fatalf("Mismatch: got - want +\n%s", diff)
	}
}
