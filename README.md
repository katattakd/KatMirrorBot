## KatMirrorBot
KatMirrorBot is an image mirroring bot that tries to mirror the *best* content from Reddit to Twitter. Used by [@it_meirl_bot](https://twitter.com/it_meirl_bot).

## Features
- Simple configuration, with support for multiple subreddits and multiple accounts.
- Easily recovers from crashes, without leaving behind temporary files.
- Has both an easy to read verbose log format, or a minimal quiet log format.
- Human-readable and easily editable data storage format, backwards-compatible with [Tootgo](https://github.com/katattakd/Tootgo).
- Fast in-RAM data storage, with extremely quick post lookups and very little disk load.
- Automatically detects the best "hot" posts to mirror, based on various metrics like upvotes, upvote rate, upvote:downvote ratio, and post age.
- Automatic post criteria detection based on other subreddit posts.
- Detects and prevents "reposts" (duplicate images) from being uploaded to the bot account.
- Detects and prevents corrupted images from being uploaded.
- Automatic post interval detection based on post depth and subreddit activity.
- Written in pure Golang with no C dependencies, to allow for easy cross-compilation.

## Compiling
1. Install and setup [Golang](https://golang.org/) for your system.
2. Download the latest code through Git (`git clone https://github.com/katattakd/KatMirrorBot.git`) or through [GitHub](https://github.com/katattakd/KatMirrorBot/archive/main.zip).
3. Open a terminal in the directory containing the code, and run `go get -d ./...` to download the code's dependencies.
4. Run the command `go build -ldflags="-s -w" -tags netgo` to compile a small static binary. Ommit `-ldflags="-s -w"` if you intend to run a debugger on the program, and ommit `-tags netgo` if a static binary isn't necessary.

## Usage
1. Get Twitter API keys from [developer.twitter.com](https://developer.twitter.com/en). How to do this is outside the scope of this README.
2. Edit the conf.json file. Fill in your API keys and the subreddits you want to mirror (by default, the program mirrors [/r/all](https://www.reddit.com/r/all) and [/r/popular](https://www.reddit.com/r/popular).
   - If you're an advanced user and intend to run multiple bots, setting `"verbose"` to `false` is highly recommended. Normal users can leave it at it's default of `true`. 
3. Run KatMirrorBot in your terminal. The program can be stopped by pressing Ctrl+C, which will also stop any downloads or uploads that are currently happening.

## Advanced usage
- If you intend to run more than one mirror bot using the program, it's recommended that you disable verbose output. Console messages from the different bots will interfere with each-other, creating a confusing mess in your terminal.
- If you intend to edit the stored posts, they're stored in the `posts.csv` file. The first (required) column contains the post's ID, and the second (optional) column contains a 64-bit difference hash of the post's image. The order of rows does not matter, however, the file should not end with a newline.
- Cross-compilation should be trivial to do (setting the `GOOS` and `GOARCH` environment variables), due to the lack of C dependencies. For a list of targets supported by the Golang compiler, run `go tool dist list`.
