package chainparse

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-github/v47/github"
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

	Latest *ChainSchema `json:"latest,omitempty"`
}

type fetcher struct {
	rt http.RoundTripper

	mu        sync.Mutex
	repoCache map[string]*github.Repository
}

func newFetcher(rt http.RoundTripper) *fetcher {
	return &fetcher{
		rt: rt,

		repoCache: make(map[string]*github.Repository),
	}
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

	return fr.traverse(ctx, registryDir)
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

func (fr *fetcher) findChainJSONFiles(ctx context.Context, registryDir string) (csL []*ChainSchema, rerr error) {
	ctx, span := trace.StartSpan(ctx, "findChainJSONFiles")
	defer span.End()

	bfs := os.DirFS(registryDir)
	err := fs.WalkDir(bfs, ".", func(path string, d fs.DirEntry, err error) (rerr error) {
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
		} else {
			csL = append(csL, cs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(csL, func(i, j int) bool {
		oi, oj := csL[i], csL[j]
		return oi.ChainName < oj.ChainName
	})

	return csL, nil
}

func (fr *fetcher) traverse(ctx context.Context, outputDir string) ([]*ChainSchema, error) {
	inputs, err := fr.findChainJSONFiles(ctx, outputDir)
	if err != nil {
		return nil, err
	}

	wg := new(sync.WaitGroup)
	inputCh := make(chan *ChainSchema, 10)
	outputCh := make(chan *ChainSchema, 1)
	go func() {
		defer close(outputCh)
		defer wg.Wait()
		defer close(inputCh)

		for _, cs := range inputs {
			inputCh <- cs
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for cs := range inputCh {
		wg.Add(1)
		go func(cs *ChainSchema) {
			defer wg.Done()
			cs, err := fr.run(ctx, *cs)
			if err == nil && cs != nil {
				outputCh <- cs
			}
		}(cs)
	}

	output := make([]*ChainSchema, 0, len(inputs))
	for cs := range outputCh {
		output = append(output, cs)
	}

	sort.Slice(output, func(i, j int) bool {
		oi, oj := output[i], output[j]
		return oi.ChainName < oj.ChainName
	})
	return output, nil
}

type csErr struct {
	cs  *ChainSchema
	url string
	err error
}

func (fr *fetcher) run(ctx context.Context, seedCS ChainSchema) (*ChainSchema, error) {
	goModURL := seedCS.Codebase.GitRepoURL

	gu, err := url.Parse(goModURL)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"git_repo_url": goModURL,
		}).Error("failed to URL Parse the Github repo URL from the registry")
		return nil, err
	}

	// This is what rawGoModURL should look like at the very end:
	//      https://raw.githubusercontent.com/Agoric/ag0/agoric-3.1/go.mod
	orgRepo := strings.TrimSuffix(gu.Path, "/")

	// Derive a cancellable context from the prevailing one
	// so that an exit will end all inflight HTTP requests.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	client := &http.Client{Transport: fr.rt}

	frCh := make(chan *csErr, 1)
	go func() {
		defer close(frCh)

		rawGoModURL := &url.URL{
			Scheme: "https",
			Host:   "raw.githubusercontent.com",
			Path:   orgRepo + "/" + seedCS.Codebase.RecommendedVersion + "/go.mod",
		}
		url := rawGoModURL.String()
		cs, err := fr.retrieveModFile(ctx, client, url, seedCS)
		frCh <- &csErr{
			url: url,
			cs:  cs,
			err: err,
		}
	}()

	latestCh := make(chan *csErr, 1)
	go func() (cs *ChainSchema, err error) {
		var uri string

		defer func() {
			latestCh <- &csErr{cs: cs, err: err, url: uri}
			close(latestCh)
		}()

		err = errors.New("skipping")
		return

		// 1. Retrieve the default branch for the repository.
		defaultBranch, err := fr.defaultBranchForRepo(ctx, orgRepo, cs.Codebase.GitRepoURL)
		if err != nil {
			return nil, err
		}

		// 2. Finally fetch the default branch's go.mod file.
		latestGoModURL := &url.URL{
			Scheme: "https",
			Host:   "raw.githubusercontent.com",
			Path:   orgRepo + "/" + defaultBranch + "/go.mod",
		}
		uri = latestGoModURL.String()
		return fr.retrieveModFile(ctx, client, uri, seedCS)
	}()

	faceValueCSE := <-frCh
	if err := faceValueCSE.err; err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"org_repo": orgRepo,
		}).Error("failed to version from the chain-registry")
		return nil, err
	}

	if faceValueCSE.cs == nil {
		return nil, fmt.Errorf("could not obtain the chainSchema for: %q", orgRepo)
	}

	lcse := <-latestCh
	if lcse.err != nil {
		// Some repos don't even exist like:
		//      https://github.com/AIOZNetwork/go-aioz
		// but if we can't get the latest schema we shouldn't error.
		logrus.WithContext(ctx).WithError(lcse.err).WithFields(logrus.Fields{
			"org_repo": orgRepo,
		}).Error("failed to get the latest/live go.mod")
	}

	// Replace the authoritative ChainSchema with
	// the version retrieved from the ChainRegistry
	// at face value.
	cs := faceValueCSE.cs
	if lcse != nil && lcse.cs != nil && !reflect.DeepEqual(cs, lcse.cs) {
		cs.Latest = lcse.cs
	}
	return cs, nil
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
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"path":         path,
				"git_repo_url": goModURL,
			}).Error("failed to URL Parse the Github repo URL from the registry")
			return nil
		}

		// This is what rawGoModURL should look like at the very end:
		//      https://raw.githubusercontent.com/Agoric/ag0/agoric-3.1/go.mod
		orgRepo := strings.TrimSuffix(gu.Path, "/")

		// Derive a cancellable context from the prevailing one
		// so that an exit will end all inflight HTTP requests.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		client := &http.Client{Transport: fr.rt}

		seedCS := *cs

		type csErr struct {
			cs  *ChainSchema
			url string
			err error
		}
		frCh := make(chan *csErr, 1)
		go func() {
			defer close(frCh)

			rawGoModURL := &url.URL{
				Scheme: "https",
				Host:   "raw.githubusercontent.com",
				Path:   orgRepo + "/" + cs.Codebase.RecommendedVersion + "/go.mod",
			}
			url := rawGoModURL.String()
			cs, err := fr.retrieveModFile(ctx, client, url, seedCS)
			frCh <- &csErr{
				url: url,
				cs:  cs,
				err: err,
			}
		}()

		latestCh := make(chan *csErr, 1)
		go func() (cs *ChainSchema, err error) {
			var uri string

			defer func() {
				latestCh <- &csErr{cs: cs, err: err, url: uri}
				close(latestCh)
			}()

			// 1. Retrieve the default branch for the repository.
			defaultBranch, err := fr.defaultBranchForRepo(ctx, orgRepo, cs.Codebase.GitRepoURL)
			if err != nil {
				return nil, err
			}

			// 2. Finally fetch the default branch's go.mod file.
			latestGoModURL := &url.URL{
				Scheme: "https",
				Host:   "raw.githubusercontent.com",
				Path:   orgRepo + "/" + defaultBranch + "/go.mod",
			}
			uri = latestGoModURL.String()
			return fr.retrieveModFile(ctx, client, uri, seedCS)
		}()

		faceValueCSE := <-frCh
		if err := faceValueCSE.err; err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"org_repo": orgRepo,
			}).Error("failed to version from the chain-registry")
			return nil
		}
		if faceValueCSE.cs == nil {
			return nil
		}

		lcse := <-latestCh
		if lcse.err != nil {
			// Some repos don't even exist like:
			//      https://github.com/AIOZNetwork/go-aioz
			// but if we can't get the latest schema we shouldn't error.
			logrus.WithContext(ctx).WithError(lcse.err).WithFields(logrus.Fields{
				"org_repo": orgRepo,
			}).Error("failed to get the latest/live go.mod")
		}

		// Replace the authoritative ChainSchema with
		// the version retrieved from the ChainRegistry
		// at face value.
		cs = faceValueCSE.cs
		if lcse != nil && lcse.cs != nil && !reflect.DeepEqual(cs, lcse.cs) {
			cs.Latest = lcse.cs
		}
		csL = append(csL, cs)
		return nil
	})

	return
}

func (fr *fetcher) retrieveModFile(ctx context.Context, client *http.Client, url string, seed ChainSchema) (*ChainSchema, error) {
	modReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	cs := new(ChainSchema)
	*cs = seed
	modRes, err := client.Do(modReq)
	if err != nil {
		return nil, err
	}
	if modRes.StatusCode < 200 || modRes.StatusCode > 299 {
		return nil, nil
	}
	modBlob, err := io.ReadAll(modRes.Body)
	modRes.Body.Close()
	if err != nil {
		return nil, err
	}
	modF, err := modfile.Parse("go.mod", modBlob, nil)
	if err != nil {
		return nil, err
	}

	cosmosSDKVers, tendermintVers, ibcVers := extractCosmosTuples(modF)

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
	return cs, nil
}

func (fr *fetcher) defaultBranchForRepo(ctx context.Context, orgRepo, repoURL string) (string, error) {
	// 1. A problem we encounter is that we run into API quota limits
	// when we invoke the https://api.github.com/repos/{org}/{repo}/ link
	// thus:
	// * Firstly try and see if the go.mod file exists on commonly
	// 2. As the last resort, actually fetch from the Github repo API.
	// In order to bypass Github API quota limits, we have to become inventive and instead
	// use a shallow git clone eliminating blobs of a big size so that the operation downloads
	// only a few kilobytes:
	//
	//	git clone --no-checkout --filter=blob:60 <URL>
	tmpDirName := strings.ReplaceAll(orgRepo, string(os.PathSeparator), "-")
	tmpDir, err := os.MkdirTemp(os.TempDir(), tmpDirName)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.CommandContext(ctx,
		"git", "clone", "--no-checkout", "--filter=blob:limit=40", repoURL, tmpDir,
	)
	if _, err := cmd.CombinedOutput(); err != nil {
		return "", err
	}

	// Now just read the .git/HEAD file.
	gitHEAD, err := os.ReadFile(filepath.Join(tmpDir, ".git", "HEAD"))
	if err != nil {
		return "", err
	}
	// Expecting the form:
	//    ref: refs/heads/Agoric
	splits := strings.Split(string(gitHEAD), ":")
	if len(splits) != 2 {
		return "", fmt.Errorf("could not split the .git/HEAD file, got: %s", gitHEAD)
	}
	i := strings.LastIndex(splits[1], "/")
	refsOfHead := strings.TrimSpace(splits[1][i+1:])
	return refsOfHead, nil
}

func (fr *fetcher) githubFetchDefaultBranchForRepo(ctx context.Context, client *http.Client, orgRepo string) (string, error) {
	// 1. Firstly check if the repository was cached or not.
	fr.mu.Lock()
	repo, ok := fr.repoCache[orgRepo]
	fr.mu.Unlock()

	if ok && repo != nil {
		return repo.GetDefaultBranch(), nil
	}

	apiURL := "https://api.github.com/repos" + orgRepo
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	blob, err := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		errStr := res.Status
		if len(blob) != 0 {
			errStr = string(blob)
		}
		return "", errors.New(errStr)
	}

	repo = new(github.Repository)
	if err := json.Unmarshal(blob, repo); err != nil {
		return "", err
	}

	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.repoCache[orgRepo] = repo

	return repo.GetDefaultBranch(), nil
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
