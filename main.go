package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/alecthomas/kingpin.v2"
)

type Video struct {
	Id           string
	Title        string
	Actress      string
	InfoHash     string
	TorrentURL   string
	TorrnetTitle string
	Size         uint
}

func (v Video) Magnet() string {
	return "magnet:?xt=urn:btih:" + v.InfoHash + "&dn=" + url.QueryEscape(v.Id)
}

type nyaaResult struct {
	Title      string
	TorrentURL string
	Size       uint
	IsTrusted  bool
}

func (result *nyaaResult) isValid(video *Video) bool {
	transf := regexp.MustCompile("[-\\s]+")
	re, err := regexp.Compile("(?i)" + transf.ReplaceAllString(video.Id, "\\s*[-\\s0]*\\s*"))
	if err != nil {
		// We cannot construct a validator, so we assume it's valid
		return true
	}
	return re.MatchString(result.Title)
}

type BySize []*nyaaResult

func (s BySize) Len() int {
	return len(s)
}

func (s BySize) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s BySize) Less(i, j int) bool {
	return s[i].Size < s[j].Size
}

type videoCache map[string]*Video

func (cache videoCache) readIn() error {
	if *cacheFile != nil {
		fi, err := (*cacheFile).Stat()
		if err != nil {
			return errors.New("Error accessing cache file")
		}
		if fi.Size() > 0 {
			err = json.NewDecoder(*cacheFile).Decode(&cache)
			if err != nil {
				return errors.New("Failed to load cache file")
			}
		}
	}
	return nil
}

func (cache videoCache) writeOut() {
	if *cacheFile != nil {
		(*cacheFile).Truncate(0)
		cacheMutex.RLock()
		cacheJson, _ := json.MarshalIndent(cache, "", "    ")
		cacheMutex.RUnlock()
		(*cacheFile).WriteAt(cacheJson, 0)
	}
}

func (cache videoCache) hasId(id string) (exists bool) {
	cacheMutex.RLock()
	_, exists = cache[id]
	cacheMutex.RUnlock()
	return
}

func (cache videoCache) addVideo(video *Video) {
	cacheMutex.Lock()
	cache[video.Id] = video
	cacheMutex.Unlock()
	cache.writeOut()
}

const javBase = "http://www.javlibrary.com"
const requestLimit = 20

var (
	cache        = make(videoCache)
	cacheFile    = kingpin.Flag("cache", "JSON file stores videos meta info").Short('c').OpenFile(os.O_CREATE|os.O_RDWR, 0600)
	magnetFile   = kingpin.Flag("out", "File to store magnet links").Short('o').OpenFile(os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	actresses    = kingpin.Flag("actress", "List of JavLibrary actress ID").Short('a').Strings()
	labels       = kingpin.Flag("label", "List of JavLibrary label ID").Short('l').Strings()
	makers       = kingpin.Flag("maker", "List of JavLibrary maker ID").Short('m').Strings()
	requestMutex = make(chan bool, requestLimit)
	cacheMutex   = new(sync.RWMutex)
)

func request(url string) (resp *http.Response, err error) {
	requestMutex <- true
	resp, err = http.Get(url)
	<-requestMutex
	if err == nil && resp.StatusCode != 200 {
		err = errors.New("Request failed: " + url +
			"\nStatus code: " + strconv.Itoa(resp.StatusCode))
	}
	return
}

func crawlJavPage(uri string, videos chan *Video, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()
	resp, err := request(uri)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile("^\\s*(.*?)\\s+(所演出的影片|所发行的影片|所制作的影片)$")
	actress := re.FindStringSubmatch(doc.Find(".boxtitle").Text())[1]

	doc.Find(".video").Each(func(i int, s *goquery.Selection) {
		id := s.Find(".id").Text()
		if !cache.hasId(id) {
			v := &Video{
				Id:      id,
				Title:   s.Find(".title").Text(),
				Actress: actress,
			}
			cache.addVideo(v)
			videos <- v
		}
	})

	nextPage := doc.Find(".page.next")
	if nextPage.Length() > 0 {
		rel, _ := nextPage.Attr("href")
		crawlJavPage(javBase+rel, videos, wg)
	}
}

func parseSize(expr string) uint {
	re := regexp.MustCompile("^(.*?)\\s+(.*?)$")
	ma := re.FindStringSubmatch(expr)
	quantity, err := strconv.ParseFloat(ma[1], 64)
	if err != nil {
		return 0
	}
	unit := ma[2]
	var multiplier float64
	switch unit {
	case "KiB":
		multiplier = 1024
	case "MiB":
		multiplier = 1048576
	case "GiB":
		multiplier = 1073741824
	}
	return uint(quantity * multiplier)
}

func crawlTorrentPage(video *Video, torrents chan *Video, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { torrents <- video }()

	resp, err := request("https://sukebei.nyaa.se/?page=search&cats=8_30&sort=5&term=" + video.Id)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		log.Fatal(err)
	}

	var results []*nyaaResult

	if doc.Find(".container").Length() > 0 {
		// Single result page
		torrentURL, _ := doc.Find(".viewdownloadbutton a").Attr("href")
		result := &nyaaResult{
			Title:      doc.Find(".viewtorrentname").Text(),
			TorrentURL: "https:" + torrentURL,
			Size:       parseSize(doc.Find(".viewtable .vtop").Last().Text()),
			IsTrusted:  doc.Find(".content").HasClass("trusted"),
		}
		if result.isValid(video) {
			results = append(results, result)
		}
	} else {
		// List result page
		doc.Find(".tlistrow").Each(func(i int, s *goquery.Selection) {
			torrentURL, _ := s.Find(".tlistdownload a").Attr("href")
			result := &nyaaResult{
				Title:      s.Find(".tlistname").Text(),
				TorrentURL: "https:" + torrentURL,
				Size:       parseSize(s.Find(".tlistsize").Text()),
				IsTrusted:  s.HasClass("trusted"),
			}
			if result.isValid(video) {
				results = append(results, result)
			}
		})
	}

	if len(results) == 0 {
		return
	}

	sort.Sort(sort.Reverse(BySize(results)))

	best := results[0]
	if !best.IsTrusted {
		for _, result := range results[1:] {
			if result.IsTrusted && (best.Size-result.Size) <= 104857600 {
				// With 100 MiB tolerance
				best = result
				break
			}
		}
	}

	resp, err = request(best.TorrentURL)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	meta, err := DecodeTorrent(resp.Body)
	if err != nil {
		return
	}

	video.TorrentURL = best.TorrentURL
	video.TorrnetTitle = meta.Info.Name
	video.InfoHash = fmt.Sprintf("%X", meta.InfoHash)
	video.Size = best.Size
}

func crawTorrent(videos chan *Video, torrents chan *Video) {
	wg := new(sync.WaitGroup)
	for v := range videos {
		wg.Add(1)
		go crawlTorrentPage(v, torrents, wg)
	}
	wg.Wait()
	close(torrents)
}

func init() {
	// Auto-run before main
	kingpin.Parse()
	cache.readIn()
}

func main() {
	videos := make(chan *Video)
	wg := new(sync.WaitGroup)

	for _, actress := range *actresses {
		go crawlJavPage(javBase+"/cn/vl_star.php?s="+actress, videos, wg)
	}

	for _, label := range *labels {
		go crawlJavPage(javBase+"/cn/vl_label.php?l="+label, videos, wg)
	}

	for _, maker := range *makers {
		go crawlJavPage(javBase+"/cn/vl_maker.php?m="+maker, videos, wg)
	}

	go func(wg *sync.WaitGroup) {
		wg.Wait()
		close(videos)
	}(wg)

	torrents := make(chan *Video)
	go crawTorrent(videos, torrents)

	for v := range torrents {
		fmt.Printf("%q\n", *v)
		if *magnetFile != nil && v.InfoHash != "" {
			(*magnetFile).WriteString(v.Magnet() + "\n")
		}
	}

	cache.writeOut()
}
