package importer

import (
	"context"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/PlakarKorp/integration-ftp/common"
	"github.com/PlakarKorp/kloset/objects"
	"github.com/PlakarKorp/kloset/snapshot/importer"
	"github.com/secsy/goftp"
)

type FTPImporter struct {
	host     string
	rootDir  string
	client   *goftp.Client
	username string
	password string
}

func NewFTPImporter(appCtx context.Context, opts *importer.Options, name string, config map[string]string) (importer.Importer, error) {
	target := config["location"]
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	username := config["username"]
	password := config["password"]

	return &FTPImporter{
		host:     parsed.Host,
		rootDir:  parsed.Path,
		username: username,
		password: password,
	}, nil
}

func (p *FTPImporter) emitParentDirectories(results chan<- *importer.ScanResult) {
	visited := make(map[string]bool)

	// Always emit root "/"
	if _, seen := visited["/"]; !seen {
		if info, err := p.client.Stat("/"); err == nil {
			fileinfo := objects.FileInfoFromStat(info)
			results <- importer.NewScanRecord("/", "", fileinfo, nil, nil)
		} else {
			// fallback to synthetic dir record
			fileinfo := objects.FileInfo{}
			fileinfo.Lname = "/"
			fileinfo.Lmode = os.ModeDir | 0o755
			fileinfo.Lsize = -1
			results <- importer.NewScanRecord("/", "", fileinfo, nil, nil)
		}
		visited["/"] = true
	}

	cleaned := path.Clean(p.rootDir)
	if cleaned == "/" {
		return
	}

	// Emit all ancestors of rootDir, top-down
	parts := strings.Split(cleaned, "/")
	currPath := "/"

	for _, part := range parts {
		if part == "" {
			continue
		}
		currPath = path.Join(currPath, part)
		if visited[currPath] {
			continue
		}

		if info, err := p.client.Stat(currPath); err == nil {
			fileinfo := objects.FileInfoFromStat(info)
			results <- importer.NewScanRecord(currPath, "", fileinfo, nil, nil)
		} else {
			// fallback to synthetic directory record
			fileinfo := objects.FileInfo{}
			fileinfo.Lname = path.Base(currPath)
			fileinfo.Lmode = os.ModeDir | 0o755
			fileinfo.Lsize = -1
			results <- importer.NewScanRecord(currPath, "", fileinfo, nil, nil)
		}

		visited[currPath] = true
	}
}

func (p *FTPImporter) walkAndCollectFiles(root string, filePaths chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	entries, err := p.client.ReadDir(root)
	if err != nil {
		log.Printf("[FTPImporter] Error reading directory %s: %v", root, err)
		return
	}

	for _, entry := range entries {
		entryPath := path.Join(root, entry.Name())

		if entry.IsDir() {
			wg.Add(1)
			go p.walkAndCollectFiles(entryPath, filePaths, wg)
		} else {
			filePaths <- entryPath
		}
	}
}

func (p *FTPImporter) processFiles(filePaths <-chan string, results chan<- *importer.ScanResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for filePath := range filePaths {
		info, err := p.client.Stat(filePath)
		if err != nil {
			results <- importer.NewScanError(filePath, err)
			continue
		}

		fileinfo := objects.FileInfoFromStat(info)

		readerFunc := func() (io.ReadCloser, error) {
			tmpfile, err := os.CreateTemp("", "plakar-ftp-")
			if err != nil {
				return nil, err
			}

			if err := p.client.Retrieve(filePath, tmpfile); err != nil {
				tmpfile.Close()
				os.Remove(tmpfile.Name())
				return nil, err
			}
			tmpfile.Seek(0, 0)

			return readerCloser{File: tmpfile}, nil
		}

		results <- importer.NewScanRecord(filePath, "", fileinfo, nil, readerFunc)
	}
}

type readerCloser struct {
	File *os.File
}

func (rc readerCloser) Read(p []byte) (int, error) {
	return rc.File.Read(p)
}

func (rc readerCloser) Close() error {
	name := rc.File.Name()
	err := rc.File.Close()
	_ = os.Remove(name)
	return err
}

func (p *FTPImporter) Scan(ctx context.Context) (<-chan *importer.ScanResult, error) {
	client, err := common.ConnectToFTP(p.host, p.username, p.password)
	if err != nil {
		return nil, err
	}
	p.client = client

	results := make(chan *importer.ScanResult, 1000)
	filePaths := make(chan string, 1000)

	var (
		workerWG sync.WaitGroup
		walkerWG sync.WaitGroup
	)

	// Emit all parent directories before file traversal begins
	p.emitParentDirectories(results)

	// Walk directory tree
	walkerWG.Add(1)
	go p.walkAndCollectFiles(p.rootDir, filePaths, &walkerWG)

	// Close filePaths only after all walk goroutines are done
	go func() {
		walkerWG.Wait()
		close(filePaths)
	}()

	// Launch worker goroutines to process file paths
	numWorkers := 64
	for range numWorkers {
		workerWG.Add(1)
		go p.processFiles(filePaths, results, &workerWG)
	}

	// Close results channel after all workers complete
	go func() {
		workerWG.Wait()
		close(results)
	}()

	return results, nil
}

func (p *FTPImporter) NewReader(pathname string) (io.ReadCloser, error) {
	tmpfile, err := os.CreateTemp("", "plakar-ftp-")
	if err != nil {
		return nil, err
	}

	if err := p.client.Retrieve(pathname, tmpfile); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		return nil, err
	}

	tmpfile.Seek(0, 0)
	return readerCloser{File: tmpfile}, nil
}

func (p *FTPImporter) Close(ctx context.Context) error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

func (p *FTPImporter) Root(ctx context.Context) (string, error) {
	return p.rootDir, nil
}

func (p *FTPImporter) Origin(ctx context.Context) (string, error) {
	return p.host, nil
}

func (p *FTPImporter) Type(ctx context.Context) (string, error) {
	return "ftp", nil
}
