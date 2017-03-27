package main

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"

	"github.com/jackpal/bencode-go"
)

var (
	ErrInvalidFormat = errors.New("Invalid torrent data")
)

// Structs into which torrent metafile is
// parsed and stored into.
type FileDict struct {
	Length int64    "length"
	Path   []string "path"
	Md5sum string   "md5sum"
}

type InfoDict struct {
	FileDuration []int64 "file-duration"
	FileMedia    []int64 "file-media"
	// Single file
	Name   string "name"
	Length int64  "length"
	Md5sum string "md5sum"
	// Multiple files
	Files       []FileDict "files"
	PieceLength int64      "piece length"
	Pieces      string     "pieces"
	Private     int64      "private"
}

type MetaInfo struct {
	Info         InfoDict   "info"
	InfoHash     string     "info hash"
	Announce     string     "announce"
	AnnounceList [][]string "announce-list"
	CreationDate int64      "creation date"
	Comment      string     "comment"
	CreatedBy    string     "created by"
	Encoding     string     "encoding"
}

// Open .torrent stream, un-bencode it and load them into MetaInfo struct.
func DecodeTorrent(r io.Reader) (*MetaInfo, error) {
	var metaInfo MetaInfo

	// Decode bencoded metainfo file.
	fileMetaData, err := bencode.Decode(r)
	if err != nil {
		return nil, err
	}

	// fileMetaData is map of maps of... maps. Get top level map.
	metaInfoMap, ok := fileMetaData.(map[string]interface{})
	if !ok {
		return nil, ErrInvalidFormat
	}

	// Enumerate through child maps.
	var bytesBuf bytes.Buffer
	for mapKey, mapVal := range metaInfoMap {
		switch mapKey {
		case "info":
			if err = bencode.Marshal(&bytesBuf, mapVal); err != nil {
				return nil, ErrInvalidFormat
			}

			infoHash := sha1.New()
			infoHash.Write(bytesBuf.Bytes())
			metaInfo.InfoHash = string(infoHash.Sum(nil))

			if err = bencode.Unmarshal(&bytesBuf, &metaInfo.Info); err != nil {
				return nil, ErrInvalidFormat
			}

		case "announce-list":
			if err = bencode.Marshal(&bytesBuf, mapVal); err != nil {
				return nil, ErrInvalidFormat
			}
			if err = bencode.Unmarshal(&bytesBuf, &metaInfo.AnnounceList); err != nil {
				return nil, ErrInvalidFormat
			}

		case "announce":
			if metaInfo.Announce, ok = mapVal.(string); !ok {
				return nil, ErrInvalidFormat
			}

		case "creation date":
			if metaInfo.CreationDate, ok = mapVal.(int64); !ok {
				return nil, ErrInvalidFormat
			}

		case "comment":
			if metaInfo.Comment, ok = mapVal.(string); !ok {
				return nil, ErrInvalidFormat
			}

		case "created by":
			if metaInfo.CreatedBy, ok = mapVal.(string); !ok {
				return nil, ErrInvalidFormat
			}

		case "encoding":
			if metaInfo.Encoding, ok = mapVal.(string); !ok {
				return nil, ErrInvalidFormat
			}
		}
	}

	return &metaInfo, nil
}

// Splits pieces string into an array of 20 byte SHA1 hashes.
func (metaInfo *MetaInfo) getPiecesList() []string {
	var piecesList []string
	piecesLen := len(metaInfo.Info.Pieces)
	for i, j := 0, 0; i < piecesLen; i, j = i+20, j+1 {
		piecesList = append(piecesList, metaInfo.Info.Pieces[i:i+19])
	}
	return piecesList
}

func (metaInfo *MetaInfo) MagnetLink() string {
	return fmt.Sprintf("magnet:?xt=urn:btih:%X", metaInfo.InfoHash)
}
