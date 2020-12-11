## KatMirrorBot
KatMirrorBot is an image mirroring bot that tries to mirror the *best* content from Reddit to Twitter. Used by [@it_meirl_bot](https://twitter.com/it_meirl_bot).

## Features
- Extremely simple configuration.
- Only runs during posting, freeing up idle memory usage for other programs.
- Easily recovers from crashes and network errors, without leaving behind temporary files.
- Human-readable and easily editable data storage format, backwards-compatible with [Tootgo](https://github.com/katattakd/Tootgo).
- Automatically detects the best "hot" posts to mirror, based on various metrics like upvotes, upvote rate, upvote:downvote ratio, and post age.
- Automatic post criteria detection based on other subreddit posts.
- Detects and prevents "reposts" (duplicate images) from being uploaded to the bot account.
- Automatically fixes corrupted images, and discards those that can't be fixed.
- Automatically optimized uploaded images for Twitter.
- Written in pure Golang with no C dependencies, to allow for easy cross-compilation.

## Compiling
1. Install and setup [Golang](https://golang.org/) for your system.
2. Download the latest code through Git (`git clone https://github.com/katattakd/KatMirrorBot.git`) or through [GitHub](https://github.com/katattakd/KatMirrorBot/archive/main.zip).
3. Open a terminal in the directory containing the code, and run `go get -d ./...` to download the code's dependencies.
4. Run the command `go build -ldflags="-s -w" -tags netgo` to compile a small static binary. Ommit `-ldflags="-s -w"` if you intend to run a debugger on the program, and ommit `-tags netgo` if a static binary isn't necessary.

## Usage
1. Get Twitter API keys from [developer.twitter.com](https://developer.twitter.com/en). How to do this is outside the scope of this README.
2. Edit the conf.json file. Fill in your API keys and the subreddits you want to mirror (by default, the program mirrors [/r/all](https://www.reddit.com/r/all) and [/r/popular](https://www.reddit.com/r/popular).
3. Test your configuration by running the KatMirrorBot in your terminal.
4. Move your configuration file, database file, and the KatMirrorBot folder (but not it's contents) to your user's home folder.
4. Add the following lines to your crontab (`crontab -e`):
```cron
5,15,25,35,45,55 * * * * ~/KatMirrorBot/KatMirrorBot conf.json 10 > mirror_5m.log 2>&1
10,50            * * * * ~/KatMirrorBot/KatMirrorBot conf.json 20 > mirror_10m.log 2>&1
20,40            * * * * ~/KatMirrorBot/KatMirrorBot conf.json 30 > mirror_20m.log 2>&1
30               * * * * ~/KatMirrorBot/KatMirrorBot conf.json 40 > mirror_30m.log 2>&1
0                * * * * ~/KatMirrorBot/KatMirrorBot conf.json 50 > mirror_60m.log 2>&1
```
This allows the bot to adapt it's posting interval and post depth as necessary, and prevents the bot from running twice.

## Advanced usage
- `config.reddit.analysisdepth` changes how many posts are downloaded and analyzed. A lower number makes the bot more strict with what posts it accepts, however, a very low analysis depth can cause issues. Should be between 20-100 posts. 
- If you intend to edit the stored posts, they're stored in the `posts.csv` file. The first (required) column contains the post's ID, and the second (optional) column contains a 256-bit perception hash of the post's image. The order of rows does not matter, however, the file should not end with a newline.
- Cross-compilation should be trivial to do (setting the `GOOS` and `GOARCH` environment variables), due to the lack of C dependencies. For a list of targets supported by the Golang compiler, run `go tool dist list`.
- The arguments KatMirrorBot accepts are a config file and a post depth limit. If omitted, the config file defaults to `conf.json`, and the depth limit defaults to 50.
