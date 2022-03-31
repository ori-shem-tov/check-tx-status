# check-tx-status

## Install
```sh
cd cmd/checktxstatus/
go install .
```

## Usage
```
➤ checktxstatus --help                                                                                                                                                                                                                   (base) 16:23:04
CLI for checking if transactions are successfully submitted to the blockchain

Usage:
  checktxstatus <file1.tx> <file2.tx> ... [flags]

Flags:
  -h, --help               help for checktxstatus
      --idx-addr string    address of the indexer client (default "https://algoindexer.algoexplorerapi.io")
      --idx-tkn string     API token of the indexer client
      --log-level string   log level: INFO or DEBUG (default "INFO")
```
