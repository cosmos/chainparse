package chainparse

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.opencensus.io/trace"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/sirupsen/logrus"
)

const registryZipURL = "https://github.com/cosmos/chain-registry/archive/refs/heads/master.zip"

type Codebase struct {
	GitRepoURL         string   `json:"git_repo"`
	RecommendedVersion string   `json:"recommended_version"`
	CompatibleVersions []string `json:"compatible_versions"`
}

type ChainSchema struct {
	ChainName         string    `json:"chain_name,omitempty"`
	NetworkType       string    `json:"network_type,omitempty"`
	Status            string    `json:"status,omitempty"`
	PrettyName        string    `json:"pretty_name,omitempty"`
	Bech32Prefix      string    `json:"bech32_prefix,omitempty"`
	Codebase          *Codebase `json:"codebase,omitempty"`
	AccountManager    string    `json:"account_manager,omitempty"`
	IsMainnet         string    `json:"is_mainnet,omitempty"`
	TendermintVersion string    `json:"tendermint_version,omitempty"`
	CosmosSDKVersion  string    `json:"cosmos_sdk_version,omitempty"`
	IBCVersion        string    `json:"ibc_version,omitempty"`
	Contact           string    `json:"contact,omitempty"`
	AccountManageer   string    `json:"account_mgr,omitempty"`
}

type fetcher struct {
	rt http.RoundTripper
}

func (fr *fetcher) fetchChainData(ctx context.Context) ([]*ChainSchema, error) {
	ctx, span := trace.StartSpan(ctx, "fetchChainData")
	defer span.End()

	registryDir, err := os.MkdirTemp(os.TempDir(), "registry")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(registryDir)

	// Download the zip file to.
	if err := fr.downloadAndUnzipRegistry(ctx, registryDir); err != nil {
		return nil, err
	}

	return fr.retrieveChainSchema(ctx, registryDir)
}

func extractCosmosTuples(modF *modfile.File) (cosmosSDKVers, tendermintVers, ibcVers string) {
	// 1. Firstly the Require directives.
	// 2. Check the Replace directives as authoritative on
	//    the final version and fork source. See https://github.com/cosmos/chainparse/issues/6

	requires := make([]module.Version, 0, len(modF.Require))
	for _, require := range modF.Require {
		requires = append(requires, require.Mod)
	}
	cosmosSDKVers, tendermintVers, ibcVers = extractCosmosTuplesByVersion(requires, false)

	replaces := make([]module.Version, 0, len(modF.Replace))
	for _, replace := range modF.Replace {
		replaces = append(replaces, replace.New)
	}
	csVersRep, tmVersRep, ibcVersRep := extractCosmosTuplesByVersion(replaces, true)

	if csVersRep != "" {
		cosmosSDKVers = csVersRep
	}
	if tmVersRep != "" {
		tendermintVers = tmVersRep
	}
	if ibcVersRep != "" {
		ibcVers = ibcVersRep
	}
	return
}

func extractCosmosTuplesByVersion(modSrcs []module.Version, isReplaceDirective bool) (cosmosSDKVers, tendermintVers, ibcVers string) {
	// 1. Firstly the Requires.
	// 2. Check the Replaces.
	for _, mod := range modSrcs {
		if !reTargets.MatchString(mod.Path) {
			continue
		}
		suffix := ""
		if isReplaceDirective {
			// For replace directives we want to append the replaced version with the URL.
			suffix = "@" + mod.Path
		}
		switch modPath := mod.Path; {
		case strings.HasSuffix(modPath, "cosmos-sdk"):
			cosmosSDKVers = mod.Version + suffix
		case strings.HasSuffix(modPath, "tendermint"):
			tendermintVers = mod.Version + suffix
		case strings.HasSuffix(modPath, "ibc-go"):
			ibcVers = mod.Version + suffix
		}
	}
	return
}

func (fr *fetcher) retrieveChainSchema(ctx context.Context, registryDir string) (csL []*ChainSchema, rerr error) {
	ctx, span := trace.StartSpan(ctx, "retrieveChainSchema")
	defer span.End()

	// 1. Git download the repo.
	// Target: https://github.com/cosmos/chain-registry/archive/refs/heads/master.zip
	bfs := os.DirFS(registryDir)
	rerr = fs.WalkDir(bfs, ".", func(path string, d fs.DirEntry, err error) (rerr error) {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(d.Name(), "chain.json") {
			return nil
		}

		f, err := bfs.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		blob, err := io.ReadAll(f)
		cs := new(ChainSchema)
		if err := json.Unmarshal(blob, cs); err != nil {
			return err
		}
		if cs.Codebase == nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"path": path,
			}).Error("No codebase")
			return nil
		}
		goModURL := cs.Codebase.GitRepoURL

		gu, err := url.Parse(goModURL)
		if err != nil {
			return err
		}

		// https://raw.githubusercontent.com/Agoric/ag0/agoric-3.1/go.mod
		rawGoModURL := &url.URL{
			Scheme: "https",
			Host:   "raw.githubusercontent.com",
			Path:   strings.TrimSuffix(gu.Path, "/") + "/" + cs.Codebase.RecommendedVersion + "/go.mod",
		}

		modReq, err := http.NewRequestWithContext(ctx, "GET", rawGoModURL.String(), nil)
		if err != nil {
			return err
		}

		client := http.Client{Transport: fr.rt}
		modRes, err := client.Do(modReq)
		if err != nil {
			return err
		}
		if modRes.StatusCode < 200 || modRes.StatusCode > 299 {
			return nil
			return fmt.Errorf("failed to parse file: %s", modRes.Status)
		}
		modBlob, err := io.ReadAll(modRes.Body)
		modRes.Body.Close()
		if err != nil {
			return err
		}
		modF, err := modfile.Parse("go.mod", modBlob, nil)
		if err != nil {
			return err
		}

		tendermintVers, cosmosSDKVers, ibcVers := extractCosmosTuples(modF)

		cs.IBCVersion = ibcVers
		cs.TendermintVersion = tendermintVers
		cs.CosmosSDKVersion = cosmosSDKVers

		// Table columns:
		// Chain,Git_Repo,Contact,Account_Manager,Is_mainnet,Mainnet GH release, CosmosSDK,Tendermint, IBC
		// var contact, accountMgr string
		isMainnet := "yes"
		if nt := cs.NetworkType; nt != "mainnet" {
			isMainnet = "no"
			if nt == "" {
				isMainnet = "?"
			}
		}
		cs.IsMainnet = isMainnet

		csL = append(csL, cs)
		return nil
	})

	return
}

var reTargets = regexp.MustCompile("cosmos-sdk|tendermint/tendermint|/ibc")

func (fr *fetcher) downloadAndUnzipRegistry(ctx context.Context, registryDir string) (rerr error) {
	ctx, span := trace.StartSpan(ctx, "downloadAndUnzipRegistry")
	defer span.End()

	defer func() {
		if rerr != nil {
			logrus.WithContext(ctx).WithError(rerr).WithFields(logrus.Fields{
				"registry_dir": registryDir,
			}).Error("download failed")
		}
	}()

	println(registryZipURL)
	req, err := http.NewRequestWithContext(ctx, "GET", registryZipURL, nil)
	if err != nil {
		return err
	}
	client := http.Client{Transport: fr.rt}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("HTTP request failed with status: %q", res.Status)
	}
	fzf, err := os.Create("registry.zip")
	if err != nil {
		return err
	}
	if _, err := io.Copy(fzf, res.Body); err != nil {
		return err
	}
	if err := fzf.Close(); err != nil {
		return err
	}
	fzf, err = os.Open("registry.zip")
	if err != nil {
		return err
	}
	defer fzf.Close()

	fi, err := fzf.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(fzf, fi.Size())
	if err != nil {
		return err
	}

	if err := os.MkdirAll(registryDir, 0755); err != nil {
		return err
	}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, "chain.json") {
			continue
		}
		fullPath := filepath.Join(registryDir, zf.Name)
		dirPath := filepath.Dir(fullPath)
		if dirPath == "" {
			continue
		}
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return err
		}
		func() {
			f, err := os.Create(fullPath)
			if err != nil {
				panic(err)
			}
			defer f.Close()

			rz, err := zf.Open()
			if err != nil {
				panic(err)
			}
			if _, err = io.Copy(f, rz); err != nil {
				panic(err)
			}
			rz.Close()
		}()
	}

	return nil
}
