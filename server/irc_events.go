package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evan-buss/openbooks/core"
)

func (server *server) NewIrcEventHandler(client *Client) core.EventHandler {
	handler := core.EventHandler{}
	handler[core.SearchResult] = client.searchResultHandler(server.config.DownloadDir)
	handler[core.BookResult] = client.bookResultHandler(server.config.DownloadDir, server.config.DisableBrowserDownloads)
	handler[core.NoResults] = client.noResultsHandler
	handler[core.BadServer] = client.badServerHandler
	handler[core.SearchAccepted] = client.searchAcceptedHandler
	handler[core.MatchesFound] = client.matchesFoundHandler
	handler[core.Ping] = client.pingHandler
	handler[core.ServerList] = client.userListHandler(server.repository)
	handler[core.Version] = client.versionHandler(server.config.UserAgent)
	handler[core.Message] = client.ircLogHandler()
	return handler
}

// ircLogHandler forwards raw IRC lines to the browser as IRC_MESSAGE
// events for the log panel. Filters down to lines actually addressed
// to the user: PRIVMSG and NOTICE whose target is our IRC nick. That
// keeps queue/download feedback ("Added ... to queueposition 5",
// "Sending you the requested file: ...", DCC SEND offers) and drops
// the firehose of channel chatter, server numerics, JOIN/PART/QUIT,
// MODE changes, and bot announcements in #ebooks.
//
// The send MUST be non-blocking: core.StartReader invokes the Message
// handler synchronously on the IRC reader goroutine (unlike all other
// handlers which it dispatches via `go invoke(...)`), so a blocking
// send here would stall the reader and starve structured events like
// DOWNLOAD. On a full per-client send channel we drop the log line.
func (c *Client) ircLogHandler() core.HandlerFunc {
	return func(text string) {
		if !isUserRelevant(text, c.irc.Username) {
			return
		}
		select {
		case c.send <- newIrcLogResponse(text):
		default:
			// Channel full; drop rather than block.
		}
	}
}

// isUserRelevant reports whether a raw IRC line is a PRIVMSG or NOTICE
// directed at our nick - i.e., a private message the user is meant to
// see. Returns false for everything else: server numerics, JOIN/PART/
// QUIT/MODE, channel-target PRIVMSGs, the connection-time "NOTICE Auth"
// ceremony, etc.
//
// Wire format we parse:
//
//	[:prefix] CMD target [params...] [:trailing]
//
// Comparison is case-insensitive because IRC nicks are.
func isUserRelevant(text, nick string) bool {
	if nick == "" {
		return false
	}
	fields := strings.Fields(text)
	var cmd, target string
	if len(fields) >= 1 && strings.HasPrefix(fields[0], ":") {
		if len(fields) < 3 {
			return false
		}
		cmd, target = fields[1], fields[2]
	} else {
		if len(fields) < 2 {
			return false
		}
		cmd, target = fields[0], fields[1]
	}
	if cmd != "PRIVMSG" && cmd != "NOTICE" {
		return false
	}
	return strings.EqualFold(target, nick)
}

// searchResultHandler downloads from DCC server, parses data, and sends data to client.
//
// Search-results zips are ephemeral - they exist only long enough to be
// parsed into the BookDetail array sent over the websocket, then deleted.
// They have no business living in the user-visible /books directory or
// any --persist'd bind mount. Routing them through os.TempDir() keeps
// them on local fast storage (typically tmpfs), avoids touching the
// possibly-network-backed mount, and means a missing or unwritable
// download directory can't break search.
func (c *Client) searchResultHandler(downloadDir string) core.HandlerFunc {
	return func(text string) {
		extractedPath, err := core.DownloadExtractDCCString(os.TempDir(), text, nil)
		if err != nil {
			c.log.Println(err)
			c.send <- newErrorResponse("Error when downloading search results.")
			return
		}

		bookResults, parseErrors, err := core.ParseSearchFile(extractedPath)
		if err != nil {
			c.log.Println(err)
			c.send <- newErrorResponse("Error when parsing search results.")
			return
		}

		if len(bookResults) == 0 && len(parseErrors) == 0 {
			c.noResultsHandler(text)
			return
		}

		// Output all errors so parser can be improved over time
		if len(parseErrors) > 0 {
			c.log.Printf("%d Search Result Parsing Errors\n", len(parseErrors))
			for _, err := range parseErrors {
				c.log.Println(err)
			}
		}

		c.log.Printf("Sending %d search results.\n", len(bookResults))
		c.send <- newSearchResponse(bookResults, parseErrors)

		err = os.Remove(extractedPath)
		if err != nil {
			c.log.Printf("Error deleting search results file: %v", err)
		}
	}
}

// bookResultHandler downloads the book file and sends it over the websocket
func (c *Client) bookResultHandler(downloadDir string, disableBrowserDownloads bool) core.HandlerFunc {
	return func(text string) {
		extractedPath, err := core.DownloadExtractDCCString(filepath.Join(downloadDir, "books"), text, nil)
		if err != nil {
			c.log.Println(err)
			c.send <- newErrorResponse("Error when downloading book.")
			return
		}

		c.log.Printf("Sending book entitled '%s'.\n", filepath.Base(extractedPath))
		c.send <- newDownloadResponse(extractedPath, disableBrowserDownloads)
	}
}

// NoResults is called when the server returns that nothing was found for the query
func (c *Client) noResultsHandler(_ string) {
	c.send <- newErrorResponse("No results found for the query.")
}

// BadServer is called when the requested download fails because the server is not available
func (c *Client) badServerHandler(_ string) {
	c.send <- newErrorResponse("Server is not available. Try another one.")
}

// SearchAccepted is called when the user's query is accepted into the search queue
func (c *Client) searchAcceptedHandler(_ string) {
	c.send <- newStatusResponse(NOTIFY, "Search accepted into the queue.")
}

// MatchesFound is called when the server finds matches for the user's query
func (c *Client) matchesFoundHandler(num string) {
	c.send <- newStatusResponse(NOTIFY, fmt.Sprintf("Found %s results for your query.", num))
}

func (c *Client) pingHandler(serverUrl string) {
	c.irc.Pong(serverUrl)
}

func (c *Client) versionHandler(version string) core.HandlerFunc {
	return func(line string) {
		c.log.Printf("Sending CTCP version response: %s", line)
		core.SendVersionInfo(c.irc, line, version)
	}
}

func (c *Client) userListHandler(repo *Repository) core.HandlerFunc {
	return func(text string) {
		repo.servers = core.ParseServers(text)
	}
}
