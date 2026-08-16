package main

import (
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/evan-buss/openbooks/server"
	"github.com/evan-buss/openbooks/util"

	"github.com/spf13/cobra"
)

var openBrowser = false
var serverConfig server.Config

func init() {
	desktopCmd.AddCommand(serverCmd)

	serverCmd.Flags().StringVarP(&serverConfig.Port, "port", "p", "5228", "Set the local network port for browser mode.")
	serverCmd.Flags().IntP("rate-limit", "r", 10, "The number of seconds to wait between searches to reduce strain on IRC search servers. Minimum is 10 seconds.")
	serverCmd.Flags().BoolVar(&serverConfig.DisableBrowserDownloads, "no-browser-downloads", false, "The browser won't recieve and download eBook files, but they are still saved to the defined 'dir' path.")
	serverCmd.Flags().StringVar(&serverConfig.Basepath, "basepath", "/", `Base path where the application is accessible. For example "/openbooks/".`)
	serverCmd.Flags().BoolVarP(&openBrowser, "browser", "b", false, "Open the browser on server start.")
	serverCmd.Flags().BoolVar(&serverConfig.Persist, "persist", false, "Persist eBooks in 'dir'. Default is to delete after sending.")
	serverCmd.Flags().StringVarP(&serverConfig.DownloadDir, "dir", "d", filepath.Join(os.TempDir(), "openbooks"), "The directory where eBooks are saved when persist enabled.")
	serverCmd.Flags().StringVar(&serverConfig.LibrarySubdir, "library-subdir", "books", "Subdirectory under --dir where downloaded books are stored and served. Empty = --dir root.")
	serverCmd.Flags().StringVar(&serverConfig.APIToken, "api-token", "", "Bearer token for the REST API under <basepath>api/. Falls back to $OPENBOOKS_API_TOKEN. Empty disables the API.")
	serverCmd.Flags().DurationVar(&serverConfig.APIIdleTimeout, "api-idle-timeout", 5*time.Minute, "Disconnect the API's IRC session after this long with no jobs.")
	serverCmd.Flags().DurationVar(&serverConfig.SearchJobTimeout, "search-job-timeout", 2*time.Minute, "Fail an API search job if the bot hasn't answered within this long.")
	serverCmd.Flags().DurationVar(&serverConfig.DownloadJobTimeout, "download-job-timeout", 10*time.Minute, "Fail an API download job if the bot hasn't offered the file within this long.")
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run OpenBooks in server mode.",
	Long:  "Run OpenBooks in server mode. This allows you to use a web interface to search and download eBooks.",
	PreRun: func(cmd *cobra.Command, args []string) {
		bindGlobalServerFlags(&serverConfig)
		rateLimit, _ := cmd.Flags().GetInt("rate-limit")
		ensureValidRate(rateLimit, &serverConfig)
		// If cli flag isn't set (default value) check for the presence of an
		// environment variable and use it if found.
		if serverConfig.Basepath == cmd.Flag("basepath").DefValue {
			if envPath, present := os.LookupEnv("BASE_PATH"); present {
				serverConfig.Basepath = envPath
			}
		}
		serverConfig.Basepath = sanitizePath(serverConfig.Basepath)
		serverConfig.APIToken = resolveAPIToken(serverConfig.APIToken)
	},
	Run: func(cmd *cobra.Command, args []string) {
		if openBrowser {
			browserUrl := "http://127.0.0.1:" + path.Join(serverConfig.Port+serverConfig.Basepath)
			util.OpenBrowser(browserUrl)
		}

		server.Start(serverConfig)
	},
}
