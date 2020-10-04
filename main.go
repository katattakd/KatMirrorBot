package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anthonynsimon/bild/transform"
	"github.com/dghubble/oauth1"
	"github.com/katattakd/go-twitter/twitter" // FIXME: Waiting for https://github.com/dghubble/go-twitter/pull/148 to be closed
	"github.com/vartanbeno/go-reddit/reddit"
)

var (
	ctx   = context.Background()
	mutex sync.RWMutex
)

type Conf struct {
	Bots []struct {
		Twitter struct {
			Token string `json:"token"`
			ToknS string `json:"tokensecret"`
			Conk  string `json:"key"`
			Cons  string `json:"keysecret"`
		} `json:"twitter"`
		Reddit struct {
			Subs []string `json:"subreddits"`
		} `json:"reddit"`
	} `json:"bots"`
	Verbose bool `json:"verbose"`
}

func loadDB(configFile string, dBfile string) (map[string]struct{}, map[int64]struct{}, Conf, *os.File) {
	fmt.Println("Loading data...")

	var config Conf
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Println("Unable to read config file! Error:\n", err)
		os.Exit(1)
	}
	if json.Unmarshal(data, &config) != nil {
		fmt.Println("Unable to parse config file! Error:\n", err)
		os.Exit(1)
	}
	if config.Verbose {
		fmt.Println("Config file loaded. Loading database...")
	}

	f, err := os.OpenFile(dBfile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Println("Unable to open database file! Error:\n", err)
		os.Exit(1)
	}

	idset := make(map[string]struct{})
	hashset := make(map[int64]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		postinfo := strings.Split(scanner.Text(), ",")
		if len(postinfo) == 1 {
			idset[postinfo[0]] = struct{}{}
		} else {
			ihash, err := strconv.ParseInt(postinfo[1], 10, 64)
			if err != nil {
				ihash = 0
			}
			idset[postinfo[0]] = struct{}{}
			hashset[ihash] = struct{}{}
		}
	}
	if config.Verbose {
		fmt.Println(len(idset), "post IDs loaded into memory,", len(hashset), "image hashes loaded into memory.\n")
	}
	return idset, hashset, config, f
}

func main() {
	idset, hashset, config, rawDB := loadDB("conf.json", "posts.csv")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	rclient, _ := reddit.NewReadonlyClient(reddit.WithHTTPClient(client))

	for i, _ := range config.Bots {
		go runBot(client, rclient, rawDB, idset, hashset, config, i)
	}

	cr := make(chan os.Signal, 1)
	signal.Notify(cr, syscall.SIGHUP)
	<-cr
}

func runBot(client *http.Client, rclient *reddit.Client, rawDB *os.File, idset map[string]struct{}, hashset map[int64]struct{}, config Conf, botIndex int) {
	var subreddit string
	for i, sub := range config.Bots[botIndex].Reddit.Subs {
		if i == 0 {
			subreddit = sub
		} else {
			subreddit = subreddit + "+" + sub
		}
	}
	if len(config.Bots[botIndex].Reddit.Subs) == 0 {
		return
	}

	oconfig := oauth1.NewConfig(config.Bots[botIndex].Twitter.Conk, config.Bots[botIndex].Twitter.Cons)
	token := oauth1.NewToken(config.Bots[botIndex].Twitter.Token, config.Bots[botIndex].Twitter.ToknS)
	tclient := twitter.NewClient(oconfig.Client(oauth1.NoContext, token))

	for {
		posts, err := getPosts(rclient, subreddit, config.Verbose)
		if err != nil {
			fmt.Println("Unable to connect to Reddit! Error:\n", err)
			time.Sleep(1 * time.Minute)
			continue
		}

		postTitle, postID, postNSFW, imageData, imageType, waitTime := getPost(posts, client, rawDB, idset, hashset, config.Verbose)
		if len(imageData) == 0 || imageType == "none" {
			if config.Verbose {
				fmt.Println("Checking for posts again in", waitTime.Round(time.Second).String()+".\n")
			} else {
				fmt.Println("No usable posts from /r/" + subreddit + ".")
			}
		} else {
			if config.Verbose {
				fmt.Println("Uploading", postID, "to twitter...")
			}

			res, _, err := tclient.Media.Upload(imageData, "image/"+imageType)
			if err == nil && res.MediaID > 0 {
				tweet, _, err := tclient.Statuses.Update(postTitle+" https://redd.it/"+postID, &twitter.StatusUpdateParams{
					MediaIds:          []int64{res.MediaID},
					PossiblySensitive: &postNSFW,
				})
				if err == nil {
					if config.Verbose {
						fmt.Println("Tweet:\n\t"+tweet.Text, "\n\thttps://twitter.com/"+tweet.User.ScreenName+"/status/"+tweet.IDStr)
					} else {
						fmt.Println(tweet.Text, "(https://twitter.com/"+tweet.User.ScreenName+"/status/"+tweet.IDStr+")")
					}
				} else {
					fmt.Println("Unable to create Tweet! Error:\n", err)
				}
			} else {
				fmt.Println("Unable to upload image to Twitter! Error:\n", err)
			}

			if config.Verbose {
				fmt.Println("Next post in", waitTime.Round(time.Second).String()+".\n")
			}
		}
		time.Sleep(waitTime)
	}
}

func getPost(posts []*reddit.Post, client *http.Client, f *os.File, idset map[string]struct{}, hashset map[int64]struct{}, verbose bool) (string, string, bool, []byte, string, time.Duration) {
	finalPostI := 0
	for i, post := range posts {
		mutex.RLock()
		_, ok := idset[post.ID]
		mutex.RUnlock()
		if ok {
			continue
		}
		if verbose {
			fmt.Println("Potentially unique post", post.ID, "found at a post depth of", i, "/", len(posts))
		}

		rawImage, err := downloadImage(post.URL, client, verbose)
		if err != nil {
			fmt.Println("Warn: Unable to download image! Error:\n", err)
			continue
		}
		if len(rawImage) == 0 {
			if verbose {
				fmt.Println("Unable to download image! Skipping post and adding ID to database.")
			}
			mutex.Lock()
			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")
			if verbose {
				fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			}
			mutex.Unlock()
			continue
		}

		imageData, imageType, err := image.Decode(bytes.NewReader(rawImage))
		if err != nil {
			if verbose {
				fmt.Println("Unable to decode image! Error:\n", err)
			}
			mutex.Lock()
			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")
			if verbose {
				fmt.Println("Skipping post and adding ID to database.\bDatabase now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			}
			mutex.Unlock()
			continue
		}
		hash := hashImage(imageData)

		mutex.RLock()
		_, ok = hashset[hash]
		mutex.RUnlock()
		if ok {
			if verbose {
				fmt.Println("Duplicate image detected, skipping post and adding ID to database.")
			}
			mutex.Lock()
			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")
			if verbose {
				fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			}
			mutex.Unlock()
			continue
		}

		if verbose {
			fmt.Println("Image (type: " + imageType + ") is valid, adding ID and hash to database...")
		}
		mutex.Lock()
		idset[post.ID] = struct{}{}
		hashset[hash] = struct{}{}
		f.WriteString(post.ID + "," + strconv.FormatInt(hash, 10) + "\n")
		if verbose {
			fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
		}
		mutex.Unlock()
		finalPostI = i

		return post.Title, post.ID, post.NSFW || post.Spoiler, rawImage, imageType, calculateSleepTime(finalPostI, len(posts))
	}
	return "", "", false, []byte{}, "none", 45 * time.Minute
}

func calculateSleepTime(i int, total int) time.Duration {
	waitTime := (float32(i) / float32(total)) * 50
	if waitTime < 5 {
		waitTime += float32(5 - int(waitTime))
	}
	if waitTime > 45 {
		waitTime -= float32(int(waitTime) - 45)
	}
	return time.Duration(waitTime*60000) * time.Millisecond
}

func downloadImage(img string, client *http.Client, verbose bool) ([]byte, error) {
	if strings.HasPrefix(img, "http://imgur.com/") {
		img = "https://i.imgur.com/" + strings.TrimPrefix(img, "http://imgur.com/") + ".jpg"
	}
	if strings.HasPrefix(img, "https://imgur.com/") {
		img = "https://i.imgur.com/" + strings.TrimPrefix(img, "https://imgur.com/") + ".jpg"
	}

	if verbose {
		fmt.Println("Downloading image from", img+"...")
	}
	resp, err := client.Get(img)
	if err != nil || resp.StatusCode >= 400 {
		if resp.Body != nil {
			resp.Body.Close()
		}
		return []byte{}, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	return body, err
}

func hashImage(img image.Image) int64 {
	image9x8 := transform.Resize(img, 9, 8, transform.Lanczos)

	bounds := image9x8.Bounds()
	hash := ""
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			if y > 0 {
				if getBrightness(image9x8.At(x, y)) > getBrightness(image9x8.At(x, y-1)) {
					hash = hash + "1"
				} else {
					hash = hash + "0"
				}
			}
		}
	}

	hashnum, _ := strconv.ParseInt(hash, 2, 64)
	return hashnum
}

func getBrightness(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return float64(r)*0.2126 + float64(g)*0.7152 + float64(b)*0.0722
}

func isImageURL(url string) bool {
	return strings.HasSuffix(url, ".png") || strings.HasSuffix(url, ".jpg") || strings.HasPrefix(url, "https://imgur.com/") || strings.HasPrefix(url, "http://imgur.com/")
}

func getPosts(client *reddit.Client, subreddit string, verbose bool) ([]*reddit.Post, error) {
	if verbose {
		fmt.Println("Downloading list of \"hot\" posts on /r/" + subreddit + "...")
	}
	posts, _, err := client.Subreddit.HotPosts(ctx, subreddit, &reddit.ListOptions{
		Limit: 100,
	})
	if err != nil {
		return []*reddit.Post{}, err
	}

	var upvoteRatios []int
	var upvoteRates []int
	var scores []int
	var ages []int
	for _, post := range posts {
		if post.IsSelfPost || post.Stickied || post.Locked || !isImageURL(post.URL) || len(post.Title) > 257 { // None of these are very useful to an image mirroring bot.
			continue
		}
		upvoteRatios = append(upvoteRatios, int(post.UpvoteRatio*100))
		upvoteRates = append(upvoteRates, int(float64(post.Score)/time.Since(post.Created.Time).Hours()))
		scores = append(scores, post.Score)
		ages = append(ages, int(time.Since(post.Created.Time).Nanoseconds()))
	}
	sort.Ints(upvoteRatios)
	sort.Ints(upvoteRates)
	sort.Ints(scores)
	sort.Ints(ages)

	if len(scores) < 10 {
		if verbose {
			fmt.Println("Analyzed 100 posts from /r/" + subreddit + ". Too few posts were usable for image mirroring.")
		}
		return []*reddit.Post{}, nil
	}

	upvoteRatioTarget := upvoteRatios[((len(upvoteRatios)*1)/10)-1]
	upvoteRateTarget := upvoteRates[((len(upvoteRates)*25)/100)-1]
	scoreTarget := scores[((len(scores)*25)/100)-1]
	ageTargetMin := time.Duration(ages[((len(ages)*1)/10)-1])
	ageTargetMax := time.Duration(ages[((len(ages)*9)/10)-1])

	if verbose {
		fmt.Println("Analyzed 100 posts from /r/"+subreddit+".", 100-len(scores), "posts were unusable for image mirroring.\nCurrent posting criteria:\n\tMinimum upvotes:", scoreTarget, "\n\tMinimum upvote rate:", upvoteRateTarget, "upvotes/hour\n\tMinimum upvote to downvote ratio:", float32(upvoteRatioTarget)/100, "\n\tAllowed post age range:", ageTargetMin.Round(time.Second), "-", ageTargetMax.Round(time.Second))
	}

	var goodPosts []*reddit.Post
	for _, post := range posts {
		if post.IsSelfPost || post.Stickied || post.Locked || !isImageURL(post.URL) || len(post.Title) > 257 || int(post.UpvoteRatio*100) < upvoteRatioTarget || post.Score < scoreTarget || float64(post.Score)/time.Since(post.Created.Time).Hours() < float64(upvoteRateTarget) || time.Since(post.Created.Time) > ageTargetMax || time.Since(post.Created.Time) < ageTargetMin {
			continue
		}
		goodPosts = append(goodPosts, post)
	}
	if verbose {
		fmt.Println(len(goodPosts), "/", len(scores), "posts met the automatically selected posting critera.\n")
	}

	return goodPosts, nil
}
