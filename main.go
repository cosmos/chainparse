package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
)

const registryZipURL = "https://github.com/cosmos/chain-registry/archive/refs/heads/master.zip"

type codebase struct {
	GitRepoURL         string   `json:"git_repo"`
	RecommendedVersion string   `json:"recommended_version"`
	CompatibleVersions []string `json:"compatible_versions"`
}

type chainSchema struct {
	ChainName    string    `json:"chain_name"`
	Status       string    `json:"status"`
	PrettyName   string    `json:"pretty_name"`
	Bech32Prefix string    `json:"bech32_prefix"`
	Codebase     *codebase `json:"codebase"`
}

var reTargets = regexp.MustCompile("cosmos-sdk|tendermint/tendermint|/ibc")

func main() {
	// 1. Git download the repo.
	// Target: https://github.com/cosmos/chain-registry/archive/refs/heads/master.zip
	bfs := os.DirFS("./registry")
	err := fs.WalkDir(bfs, ".", func(path string, d fs.DirEntry, err error) error {
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
		cs := new(chainSchema)
		if err := json.Unmarshal(blob, cs); err != nil {
			return err
		}
		if cs.Codebase == nil {
			// TODO: Report this otherwise.
			println("\033[31mNo codebase for " + path + "\033[00m")
			return nil
		}
		goModURL := cs.Codebase.GitRepoURL

		gu, err := url.Parse(goModURL)
		if err != nil {
			panic(err)
		}

		// https://raw.githubusercontent.com/Agoric/ag0/agoric-3.1/go.mod
		rawGoModURL := &url.URL{
			Scheme: "https",
			Host:   "raw.githubusercontent.com",
			Path:   gu.Path + "/" + cs.Codebase.RecommendedVersion + "/go.mod",
		}

		modRes, err := http.Get(rawGoModURL.String())
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

		matched := false
		for _, require := range modF.Require {
			if reTargets.MatchString(require.Mod.Path) {
				if !matched {
					println("\n"+cs.PrettyName, cs.Codebase.RecommendedVersion)
				}
				matched = true
				fmt.Printf("\tMod: %#v\n", require.Mod)
			}
		}
		if !matched {
			return fmt.Errorf("Nothing here")
		}

		// Find the requires for cosmos-sdk or tendermint.
		return nil
	})
	if err != nil {
		panic(err)
	}
}
