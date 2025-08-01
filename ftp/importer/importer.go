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

	var username string
	if tmp, ok := config["username"]; ok {
		username = tmp
	}

	var password string
	if tmp, ok := config["password"]; ok {
		password = tmp
	}

	return &FTPImporter{
		host:     parsed.Host,
		rootDir:  parsed.Path,
		username: username,
		password: password,
	}, nil
}

func (p *FTPImporter) ftpWalker_worker(jobs <-chan string, results chan<- *importer.ScanResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for path := range jobs {
		info, err := p.client.Stat(path)
		if err != nil {
			results <- importer.NewScanError(path, err)
			continue
		}

		fileinfo := objects.FileInfoFromStat(info)

		results <- importer.NewScanRecord(path, "", fileinfo, nil,
			func() (io.ReadCloser, error) { return p.NewReader(path) })

		// Handle symlinks separately
		if fileinfo.Mode()&os.ModeSymlink != 0 {
			originFile, err := os.Readlink(path)
			if err != nil {
				results <- importer.NewScanError(path, err)
				continue
			}
			results <- importer.NewScanRecord(path, originFile, fileinfo, nil, nil)
		}
	}
}

func (p *FTPImporter) ftpWalker_addPrefixDirectories(jobs chan<- string, results chan<- *importer.ScanResult) {
	directory := path.Clean(p.rootDir)
	atoms := strings.Split(directory, string(os.PathSeparator))

	for i := 0; i < len(atoms); i++ {
		path := path.Join(atoms[0 : i+1]...)

		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		if _, err := p.client.Stat(path); err != nil {
			results <- importer.NewScanError(path, err)
			continue
		}

		jobs <- path
	}
}

func (p *FTPImporter) walkDir(root string, results chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	entries, err := p.client.ReadDir(root)
	if err != nil {
		log.Printf("Error reading directory %s: %v", root, err)
		return
	}

	for _, entry := range entries {
		entryPath := path.Join(root, entry.Name())

		// Send the current entry to the results channel
		results <- entryPath

		// If the entry is a directory, traverse it recursively
		if entry.IsDir() {
			wg.Add(1)
			go p.walkDir(entryPath, results, wg)
		}
	}
}

func (p *FTPImporter) Scan(ctx context.Context) (<-chan *importer.ScanResult, error) {
	client, err := common.ConnectToFTP(p.host, p.username, p.password)
	if err != nil {
		return nil, err
	}
	p.client = client

	results := make(chan *importer.ScanResult, 1000) // Larger buffer for results
	jobs := make(chan string, 1000)                  // Buffered channel to feed paths to workers
	var wg sync.WaitGroup
	numWorkers := 256

	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go p.ftpWalker_worker(jobs, results, &wg)
	}

	go func() {
		defer close(jobs)
		p.ftpWalker_addPrefixDirectories(jobs, results)
		wg.Add(1)
		p.walkDir(p.rootDir, jobs, &wg)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results, nil
}

func (p *FTPImporter) NewReader(pathname string) (io.ReadCloser, error) {
	tmpfile, err := os.CreateTemp("", "plakar-ftp-")
	if err != nil {
		return nil, err
	}

	err = p.client.Retrieve(pathname, tmpfile)
	if err != nil {
		return nil, err
	}
	tmpfile.Seek(0, 0)

	return tmpfile, nil
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
