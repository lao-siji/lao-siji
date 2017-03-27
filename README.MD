# Lao-SiJi - A Go (golang) CLI crawler for old drivers. Make sure you have a driver license to use it.

## Example Usage

```shell
go run main.go infohash.go -c cache.json -o magnets.txt -a afdss -a azefi -a ayqag -a pqaq -a a5nvq -a ayera -m aq4q
```

* `-c` specifies a permanent JSON cache file for meta data output
* `-o` specifies a TXT output file for the obtained magnet links
* `-a` list of JavLibrary actress IDs to crawl
* `-m` list of JavLibrary maker IDs to crawl
* `-l` list of JavLibrary label IDs to crawl
