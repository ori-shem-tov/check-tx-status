package main

import (
	"context"
	"fmt"
	"github.com/algorand/go-algorand-sdk/client/v2/indexer"
	"github.com/algorand/go-algorand-sdk/crypto"
	"github.com/algorand/go-algorand-sdk/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/types"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

var (
	logLevelStr string

	indexerAddress string
	indexerToken   string
)

// setLogger sets the logger level based on the value from --log-level
// currently excepts INFO and DEBUG, defaults to WARN
func setLogger(logLevelStr string) {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	logLevelStr = strings.ToLower(logLevelStr)
	logLevel := log.WarnLevel
	if logLevelStr == "debug" {
		logLevel = log.DebugLevel
	} else if logLevelStr == "info" {
		logLevel = log.InfoLevel
	}
	log.SetLevel(logLevel)
}

func init() {
	rootCmd.Flags().StringVar(&logLevelStr, "log-level", "INFO", "log level: INFO or DEBUG")
	rootCmd.Flags().StringVar(&indexerAddress, "idx-addr", os.Getenv("AF_IDX_ADDRESS"), "address of the indexer client")
	rootCmd.Flags().StringVar(&indexerToken, "idx-tkn", os.Getenv("AF_IDX_TOKEN"), "API token of the indexer client")

}

// initIndexerClient inits an indexer client
// indexerAddress comes from --idx-addr flag or AF_IDX_ADDRESS environment variable
// indexerToken comes from --idx-tkn flag or AF_IDX_TOKEN environment variable
func initIndexerClient(indexerAddress, indexerToken string) (*indexer.Client, error) {

	if indexerAddress == "" {
		return nil, fmt.Errorf("please supply an indexer client address using --idx-addr flag or AF_IDX_ADDRESS environment variable")
	}

	indexerClient, err := indexer.MakeClient(indexerAddress, indexerToken)
	if err != nil {
		return nil, fmt.Errorf("failed creating the indexer client: %v", err)
	}

	return indexerClient, nil
}

// readTxFile reads and decodes trnsactions from a file, separating them to groups and individual transactions
// it assumes groups of transactions appear consecutively and does not validate them
func readTxFile(filename string) (map[types.Digest][]types.SignedTxn, []types.SignedTxn, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("error while opening %s: %v", filename, err)
	}
	// no need to check error on close when reading file
	defer file.Close()

	logger := log.WithField("file", filename)

	dec := msgpack.NewDecoder(file)

	groups := map[types.Digest][]types.SignedTxn{}
	var individualTxs []types.SignedTxn

	for {
		var stx types.SignedTxn
		err = dec.Decode(&stx) // read next transaction into stx
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Errorf("error while dcoding txn: %v", err)
			return nil, nil, err
		}

		gid := stx.Txn.Group
		if (gid == types.Digest{}) {
			individualTxs = append(individualTxs, stx)
		} else {
			groups[gid] = append(groups[gid], stx)
		}
	}
	return groups, individualTxs, nil
}

// isTxSent queries the indexer to check if transaction was sent
func isTxSent(txid string, indexerClient *indexer.Client) (bool, error) {
	_, err := indexerClient.LookupTransaction(txid).Do(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// filterUnsentGroups returns only the groups of transactions that were not sent
func filterUnsentGroups(groups map[types.Digest][]types.SignedTxn, indexerClient *indexer.Client) (map[types.Digest][]types.SignedTxn, error) {
	unsentGroups := map[types.Digest][]types.SignedTxn{}
	logger := log.WithField("function", "filterUnsentGroups")
	for gid, txs := range groups {
		if len(txs) == 0 {
			// this should never happen as we generate `groups` in `readTxFile` only if there's at least 1 tx with `gid`
			logger.Fatalf("group %s has no transactions in slice", gid)
		}
		firstTxID := crypto.GetTxID(txs[0].Txn)
		groupSent, err := isTxSent(firstTxID, indexerClient)
		if err != nil {
			return nil, fmt.Errorf("failed getting status of tx %s in group %s", firstTxID, gid)
		}
		if !groupSent {
			unsentGroups[gid] = txs
		}
	}
	return unsentGroups, nil
}

// filterUnsentTxs returns only transactions that were not sent
func filterUnsentTxs(txs []types.SignedTxn, indexerClient *indexer.Client) ([]types.SignedTxn, error) {
	var unsentTxs []types.SignedTxn
	for _, tx := range txs {
		txID := crypto.GetTxID(tx.Txn)
		isSent, err := isTxSent(txID, indexerClient)
		if err != nil {
			return nil, fmt.Errorf("failed getting status of tx %s", txID)
		}
		if !isSent {
			unsentTxs = append(unsentTxs, tx)
		}
	}
	return unsentTxs, nil
}

// flattenGroupsMap return a slice of all transactions in the given map
func flattenGroupsMap(groups map[types.Digest][]types.SignedTxn) []types.SignedTxn {
	var result []types.SignedTxn
	for _, txs := range groups {
		result = append(result, txs...)
	}
	return result
}

// writeTxsToFile writes a slice of transactions to a file
func writeTxsToFile(filename string, txs []types.SignedTxn) error {
	var toWrite []byte
	for _, tx := range txs {
		encoded := msgpack.Encode(tx)
		toWrite = append(toWrite, encoded...)
	}
	err := ioutil.WriteFile(filename, toWrite, 0600)
	if err != nil {
		return fmt.Errorf("failed to write txs to %s", filename)
	}
	return nil
}

var rootCmd = &cobra.Command{
	Use:   "checktxstatus <file1.tx> <file2.tx> ...",
	Short: "CLI for checking if transactions are successfully submitted to the blockchain",
	Run: func(cmd *cobra.Command, args []string) {
		setLogger(logLevelStr)
		indexerClient, err := initIndexerClient(indexerAddress, indexerToken)
		if err != nil {
			log.Error(err)
			return
		}
		if len(args) == 0 {
			log.Error("supply at least 1 transactions file")
			cmd.HelpFunc()(cmd, args)
		}

		for _, filename := range args {
			groups, indTxs, err := readTxFile(filename)
			if err != nil {
				log.Error(err)
				return
			}
			log.Infof("found %d groups and %d individual transactions in %s", len(groups), len(indTxs), filename)
			unsentGroups, err := filterUnsentGroups(groups, indexerClient)
			if err != nil {
				log.Error(err)
				return
			}
			unsentIndividualTxs, err := filterUnsentTxs(indTxs, indexerClient)
			if err != nil {
				log.Error(err)
				return
			}
			log.Infof("file %s has %d unsent groups and %d unsent individual transactions",
				filename, len(unsentGroups), len(unsentIndividualTxs))
			flattenUnsentGroups := flattenGroupsMap(unsentGroups)
			allUnsent := append(flattenUnsentGroups, unsentIndividualTxs...)
			if len(allUnsent) != 0 {
				unsentFilename := fmt.Sprintf("%s.unsent", filename)
				err = writeTxsToFile(unsentFilename, allUnsent)
				if err != nil {
					log.Error(err)
					return
				}
				log.Infof("wrote unsent transactions to %s", unsentFilename)
			} else {
				log.Infof("no unsent transaction were found!")
			}
		}
	},
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}
