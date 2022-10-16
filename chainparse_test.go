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

var testdataZip, testdataGoMod, testdataGithubRepo, testdataLatestGoMod []byte

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

	td, err = os.ReadFile("./testdata/registry/repos/repo.json")
	if err != nil {
		panic(err)
	}
	testdataGithubRepo = td

	td, err = os.ReadFile("./testdata/registry/mod/latestGo.mod")
	if err != nil {
		panic(err)
	}
	testdataLatestGoMod = td
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
		// 1. Service the request for the live Github repo.
		if strings.HasPrefix(req.URL.Path, "/repos") {
			rw.Write(testdataGithubRepo)
			return
		}

		if false && strings.Contains(req.URL.Path, "Agoric/ag0/main/go.mod") {
			rw.Write(testdataLatestGoMod)
			return
		}
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
	fetcher := newFetcher(art)

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
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
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
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "arkh",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Arkhadian",
			Bech32Prefix: "arkh",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/vincadian/arkh-blockchain",
				RecommendedVersion: "v1.0.0",
				CompatibleVersions: []string{"v1.0.0"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
			IBCVersion:        "v1.2.0",
		},
	}

	if diff := cmp.Diff(got[:3], wantFirst3); diff != "" {
		t.Fatalf("First 3 mismatch: got - want +\n%s", diff)
	}

	wantLast3 := []*ChainSchema{
		{
			ChainName:    "tgrade",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Tgrade",
			Bech32Prefix: "tgrade",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/confio/tgrade",
				RecommendedVersion: "v1.0.1",
				CompatibleVersions: []string{"v1.0.1"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
			IBCVersion:        "v1.2.0",
		},
		{
			ChainName:    "theta",
			NetworkType:  "mainnet",
			Status:       "live",
			PrettyName:   "Theta Testnet",
			Bech32Prefix: "theta",
			Codebase: &Codebase{
				GitRepoURL:         "https://github.com/cosmos/gaia",
				RecommendedVersion: "v7.0.2",
				CompatibleVersions: []string{"v7.0.2"},
			},
			IsMainnet:         "yes",
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
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
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
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
			TendermintVersion: "v0.34.13@github.com/tendermint/tendermint",
			CosmosSDKVersion:  "v0.44.2-alpha.agoric.gaiad.1@github.com/agoric-labs/cosmos-sdk",
			IBCVersion:        "v1.2.0",
		},
	}

	if diff := cmp.Diff(got[len(got)-3:], wantLast3); diff != "" {
		t.Fatalf("Last3 mismatch: got - want +\n%s", diff)
	}
}

func TestDefaultBranchForRepo(t *testing.T) {
	ctx := context.Background()
	fr := newFetcher(nil)
	head, err := fr.defaultBranchForRepo(ctx, "Agoric/ag0", "https://github.com/Agoric/ag0")
	if err != nil {
		t.Fatal(err)
	}
	if g, w := head, "Agoric"; g != w {
		t.Fatalf("Default branch mismatch:\n\tGot:  %q\n\tWant: %q", g, w)
	}
}
