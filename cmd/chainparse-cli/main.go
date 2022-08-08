package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmos/chainparse"
)

func main() {
	ctx := context.Background()
	csL, err := chainparse.RetrieveChainData(ctx, nil)
	if err != nil {
		panic(err)
	}

	printHeader()

	for _, cs := range csL {
		line := []string{
			cs.PrettyName, cs.Codebase.GitRepoURL, cs.Contact, cs.AccountManager, cs.IsMainnet,
			cs.Codebase.RecommendedVersion, cs.CosmosSDKVersion, cs.TendermintVersion, cs.IBCVersion,
		}
		fmt.Println(strings.Join(line, ","))
	}
}

func printHeader() {
	fmt.Println("Chain,Git_Repo,Contact,Account_Manager,Is_mainnet,Mainnet GH release,CosmosSDK,Tendermint,IBC")
}
