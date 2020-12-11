package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	"image/jpeg"
	_ "image/png"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/corona10/goimagehash"
	"github.com/dghubble/oauth1"
	"github.com/katattakd/go-twitter/twitter" // FIXME: Waiting for https://github.com/dghubble/go-twitter/pull/148 to be closed
	"github.com/vartanbeno/go-reddit/reddit"
)

type Conf struct {
	Twitter struct {
		Token string `json:"token"`
		ToknS string `json:"tokensecret"`
		Conk  string `json:"key"`
		Cons  string `json:"keysecret"`
	} `json:"twitter"`
	Reddit struct {
		Subs []string `json:"subreddits"`
	} `json:"reddit"`
	DBFile string `json:"postdb"`
}

var ctx = context.Background()

func loadData(configFile string) (map[string]struct{}, map[string]struct{}, Conf, *os.File) {
	fmt.Println("Loading " + configFile + "...")

	var config Conf
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to read config file! Error:\n", err)
		os.Exit(1)
	}
	if json.Unmarshal(data, &config) != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to parse config file! Error:\n", err)
		os.Exit(1)
	}
	fmt.Println("Program configuration loaded. Loading " + config.DBFile + "...")

	f, err := os.OpenFile(config.DBFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to open database file! Error:\n", err)
		os.Exit(1)
	}

	idset := make(map[string]struct{})
	hashset := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		postinfo := strings.Split(scanner.Text(), ",")
		if len(postinfo) == 1 {
			idset[postinfo[0]] = struct{}{}
		} else {
			idset[postinfo[0]] = struct{}{}
			hashset[postinfo[1]] = struct{}{}
		}
	}
	fmt.Println(len(idset), "post IDs loaded into memory,", len(hashset), "image hashes loaded into memory.\n\n")
	return idset, hashset, config, f
}

func getRedditPosts(config Conf) []*reddit.Post {
	var subreddit string
	for i, sub := range config.Reddit.Subs {
		if i == 0 {
			subreddit = sub
		} else if sub != "" {
			subreddit = subreddit + "+" + sub
		}
	}
	if len(subreddit) == 0 {
		fmt.Fprintln(os.Stderr, "Fatal: config.reddit.subreddits must have a length greater than zero!")
		os.Exit(1)
	}

	rclient, err := reddit.NewReadonlyClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to create Reddit client! Error:\n", err)
		os.Exit(1)
	}

	fmt.Println("Downloading list of \"hot\" posts on /r/" + subreddit + "...")
	posts, resp, err := rclient.Subreddit.HotPosts(ctx, subreddit, &reddit.ListOptions{
		Limit: 100,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to download post list! Error:\n", err)
		os.Exit(1)
	}

	resp.Body.Close()
	return posts
}

func isImageURL(url string) bool {
	return strings.HasSuffix(url, ".png") || strings.HasSuffix(url, ".jpg") || strings.HasPrefix(url, "https://imgur.com/") || strings.HasPrefix(url, "http://imgur.com/")
}

func filterRedditPosts(posts []*reddit.Post) []*reddit.Post {
	fmt.Println("Downloaded", len(posts), "posts. Analyzing and filtering posts...")

	var upvoteRatios, upvoteRates, scores, ages []int
	for _, post := range posts {
		if post.IsSelfPost || post.Stickied || post.Locked || !isImageURL(post.URL) || len(post.Title) > 257 {
			// None of these are very useful to an image mirroring bot.
			continue
		}

		upvoteRatios = append(upvoteRatios, int(post.UpvoteRatio*100))
		upvoteRates = append(upvoteRates, int(float64(post.Score)/time.Since(post.Created.Time).Hours()))
		scores = append(scores, post.Score)
		ages = append(ages, int(time.Now().UTC().Sub(post.Created.Time.UTC()).Seconds()))
	}

	sort.Ints(upvoteRatios)
	sort.Ints(upvoteRates)
	sort.Ints(scores)
	sort.Ints(ages)

	upvoteRatioTarget, upvoteRateTarget, scoreTarget, ageTargetMin := 0, 0, 0, time.Duration(0)

	if len(scores) > 4 {
		scoreTarget = scores[(len(scores)/3)-1]
		upvoteRateTarget = upvoteRates[(len(upvoteRates)/3)-1]
		fmt.Println(len(scores), "posts were usable for image mirroring.\nCurrent posting criteria:\n\tMinimum upvotes:", scoreTarget, "\n\tMinimum upvote rate:", upvoteRateTarget, "upvotes/hour")
	}
	if len(scores) > 10 {
		upvoteRatioTarget = upvoteRatios[(len(upvoteRatios)/10)-1]
		fmt.Println("\tMinimum upvote to downvote ratio:", float32(upvoteRatioTarget)/100)
	}
	if len(scores) > 6 {
		ageTargetMin = time.Duration(ages[(len(ages)/5)-1]) * time.Second
		fmt.Println("\tMinimum post age:", ageTargetMin.Round(time.Second))
	}

	var goodPosts []*reddit.Post
	for _, post := range posts {
		if post.IsSelfPost || post.Stickied || post.Locked || !isImageURL(post.URL) || len(post.Title) > 257 || int(post.UpvoteRatio*100) < upvoteRatioTarget || post.Score < scoreTarget || float64(post.Score)/time.Since(post.Created.Time).Hours() < float64(upvoteRateTarget) || time.Now().UTC().Sub(post.Created.Time.UTC()) < ageTargetMin {
			continue
		}
		goodPosts = append(goodPosts, post)
	}
	if len(scores) > 4 {
		fmt.Println(len(goodPosts), "/", len(scores), "posts met the automatically selected posting critera.")
	} else if len(scores) == 0 {
		fmt.Fprintln(os.Stderr, "Warn: No posts were usable for image mirroring.")
		os.Exit(0)
	} else {
		fmt.Println(len(goodPosts), "posts were useable for image mirroring.")
	}

	return goodPosts
}

func downloadImageURL(url string) (*http.Response, string, error) {
	if strings.HasPrefix(url, "http://imgur.com/") {
		url = "https://i.imgur.com/" + strings.TrimPrefix(url, "http://imgur.com/") + ".jpg"
	}
	if strings.HasPrefix(url, "https://imgur.com/") {
		url = "https://i.imgur.com/" + strings.TrimPrefix(url, "https://imgur.com/") + ".jpg"
	}

	fmt.Println("Downloading image from", url+"...")
	resp, err := http.DefaultClient.Get(url)

	return resp, url, err
}

func getUniqueRedditPost(posts []*reddit.Post, f *os.File, idset map[string]struct{}, hashset map[string]struct{}, postLimit int) (*reddit.Post, image.Image, string) {
	if len(posts) > postLimit {
		fmt.Println("Limiting search depth to", postLimit, "posts.")
	} else {
		postLimit = len(posts)	
	}
	for i, post := range posts {
		if i > postLimit {
			break
		}

		_, ok := idset[post.ID]
		if ok {
			continue
		}

		fmt.Println("\nPotentially unique post", post.ID, "found at a post depth of", i, "/", postLimit)

		resp, url, err := downloadImageURL(post.URL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Warn: Unable to download image! Error:\n", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			fmt.Println("Unable to download " + path.Base(url) + "! Skipping post and adding ID to database.")

			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")

			fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			continue
		}

		fmt.Println("Decoding and hashing", path.Base(url)+"...")

		imageData, imageType, err := image.Decode(resp.Body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Warn: Unable to decode image! Error:\n", err)

			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")

			fmt.Println("Skipping post and adding ID to database.\nDatabase now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			continue
		}

		hashraw, err := goimagehash.ExtPerceptionHash(imageData, 16, 16)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Warn: Unable to hash image! Error:\n", err)

			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")

			fmt.Println("Skipping post and adding ID to database.\nDatabase now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			continue
		}
		hash := hashraw.ToString()

		_, ok = hashset[hash]
		if ok {
			fmt.Println("Duplicate image detected, skipping post and adding ID to database.")

			idset[post.ID] = struct{}{}
			f.WriteString(post.ID + "\n")

			fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")
			continue
		}

		fmt.Println("Image (type: " + imageType + ") is unique, adding ID and hash to database...")

		idset[post.ID] = struct{}{}
		hashset[hash] = struct{}{}
		f.WriteString(post.ID + "," + hash + "\n")

		fmt.Println("Database now contains", len(idset), "post IDs and", len(hashset), "hashes.\n")

		return post, imageData, path.Base(url)
	}
	fmt.Fprintln(os.Stderr, "\nWarn: No unique posts were found.")
	os.Exit(0)

	return nil, nil, "" // Still have to return something, even if os.Exit() is being called.
}

func createTwitterPost(config Conf, post *reddit.Post, image image.Image, file string) {
	fmt.Println("Uploading", file, "to twitter...")

	oconfig := oauth1.NewConfig(config.Twitter.Conk, config.Twitter.Cons)
	token := oauth1.NewToken(config.Twitter.Token, config.Twitter.ToknS)
	tclient := twitter.NewClient(oconfig.Client(oauth1.NoContext, token))

	var buf bytes.Buffer
	jpeg.Encode(&buf, image, &jpeg.Options{
		/* Theoretically, yes, there is some *slight* generation loss from lossy re-encoding.
		However, with JPEG quality 100, the generation loss is imperceptable to humans, even after many re-encodes.*/
		Quality: 100,
	})

	res, resp, err := tclient.Media.Upload(buf.Bytes(), "image/jpeg")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to upload image to Twitter! Error:\n", err)
		os.Exit(1)
	}
	resp.Body.Close()

	fmt.Println("Creating tweet (PostID: " + post.ID + ")...")
	isNSFW := post.NSFW || post.Spoiler
	tweet, resp, err := tclient.Statuses.Update(post.Title+" https://redd.it/"+post.ID, &twitter.StatusUpdateParams{
		MediaIds:          []int64{res.MediaID},
		PossiblySensitive: &isNSFW,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fatal: Unable to create Tweet! Error:\n", err)
		os.Exit(1)
	}
	resp.Body.Close()

	fmt.Println("Tweet:\n\t"+tweet.Text, "\n\thttps://twitter.com/"+tweet.User.ScreenName+"/status/"+tweet.IDStr)
}

func main() {
	configFile := "conf.json"
	depthLimit := 50
	if len(os.Args[1:]) > 0 {
		configFile = os.Args[1]
	}
	if len(os.Args[1:]) > 1 {
		i, err := strconv.Atoi(os.Args[2])
		if err == nil {
			if depthLimit > 100 {
				fmt.Fprintln(os.Stderr, "Warn: Depth limit must be less than or equal to 100.")
				depthLimit = 100
			} else if depthLimit < 1 {
				fmt.Fprintln(os.Stderr, "Warn: Depth limit must be greater than 0.")
				depthLimit = 1
			} else {
				depthLimit = i
			}
		} else {
			fmt.Fprintln(os.Stderr, "Warn: Unable to parse post depth argument!")
		}
	}

	idset, hashset, config, rawDB := loadData(configFile)
	defer rawDB.Close()

	http.DefaultClient.Timeout = 30 * time.Second
	posts := filterRedditPosts(getRedditPosts(config))
	post, image, imageName := getUniqueRedditPost(posts, rawDB, idset, hashset, depthLimit)
	createTwitterPost(config, post, image, imageName)
}
