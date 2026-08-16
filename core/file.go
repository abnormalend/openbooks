package core

import (
	"io"
	"os"
	"path/filepath"

	"github.com/evan-buss/openbooks/dcc"
	"github.com/evan-buss/openbooks/util"
)

func DownloadExtractDCCString(baseDir, dccStr string, progress io.Writer) (string, error) {
	// Download the file and wait until it is completed
	download, err := dcc.ParseString(dccStr)
	if err != nil {
		return "", err
	}

	// Self-heal: ensure baseDir exists. Callers historically rely on
	// server.createBooksDirectory having fired at startup, but that's
	// fragile - if the directory is missing for any reason (volume swap,
	// non-default --dir without that dir pre-existing, manual delete),
	// the book download otherwise fails with a confusing ENOENT.
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}

	dccPath := filepath.Join(baseDir, download.Filename+".temp")
	file, err := os.Create(dccPath)
	if err != nil {
		return "", err
	}

	writer := io.Writer(file)
	if progress != nil {
		writer = io.MultiWriter(file, progress)
	}

	// Download DCC data to the file
	err = download.Download(writer)
	if err != nil {
		return "", err
	}
	file.Close()
	if !util.IsArchive(dccPath) {
		return renameTempFile(dccPath), nil
	}

	extractedPath, err := util.ExtractArchive(dccPath)
	if err != nil {
		return "", err
	}

	return renameTempFile(extractedPath), nil
}

func renameTempFile(filePath string) string {
	if filepath.Ext(filePath) == ".temp" {
		newPath := filePath[:len(filePath)-len(".temp")]
		os.Rename(filePath, newPath)
		return newPath
	}

	return filePath
}
