package importer

import (
	"context"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"sync"

	"github.com/PlakarKorp/integration-ftp/common"
	"github.com/PlakarKorp/kloset/connectors"
	"github.com/PlakarKorp/kloset/connectors/importer"
	"github.com/PlakarKorp/kloset/location"
	"github.com/PlakarKorp/kloset/objects"
	"github.com/secsy/goftp"
)

func init() {
	importer.Register("ftp", 0, NewFTPImporter)
}

type FTPImporter struct {
	host     string
	rootDir  string
	client   *goftp.Client
	username string
	password string
}

func NewFTPImporter(appCtx context.Context, opts *connectors.Options, name string, config map[string]string) (importer.Importer, error) {
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

func (p *FTPImporter) walkAndCollectFiles(ctx context.Context, root string, filePaths chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	if err := ctx.Err(); err != nil {
		return
	}

	entries, err := p.client.ReadDir(root)
	if err != nil {
		log.Printf("[FTPImporter] Error reading directory %s: %v", root, err)
		return
	}

	for _, entry := range entries {
		entryPath := path.Join(root, entry.Name())

		if entry.IsDir() {
			wg.Add(1)
			go p.walkAndCollectFiles(ctx, entryPath, filePaths, wg)
		} else {
			filePaths <- entryPath
		}
	}
}

func (p *FTPImporter) processFiles(filePaths <-chan string, results chan<- *connectors.Record, wg *sync.WaitGroup) {
	defer wg.Done()

	for filePath := range filePaths {
		info, err := p.client.Stat(filePath)
		if err != nil {
			results <- connectors.NewError(filePath, err)
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

		results <- connectors.NewRecord(filePath, "", fileinfo, nil, readerFunc)
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

func (p *FTPImporter) Import(ctx context.Context, records chan<- *connectors.Record, results <-chan *connectors.Result) error {
	client, err := common.ConnectToFTP(p.host, p.username, p.password)
	if err != nil {
		return err
	}
	p.client = client

	filePaths := make(chan string, 1000)

	var (
		workerWG sync.WaitGroup
		walkerWG sync.WaitGroup
	)

	// Walk directory tree
	walkerWG.Add(1)
	go p.walkAndCollectFiles(ctx, p.rootDir, filePaths, &walkerWG)

	// Close filePaths only after all walk goroutines are done
	go func() {
		walkerWG.Wait()
		close(filePaths)
	}()

	// Launch worker goroutines to process file paths
	numWorkers := 64
	for range numWorkers {
		workerWG.Add(1)
		go p.processFiles(filePaths, records, &workerWG)
	}

	// Close results channel after all workers complete
	go func() {
		workerWG.Wait()
		close(records)
	}()

	return nil
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

func (p *FTPImporter) Ping(ctx context.Context) error {
	return nil
}

func (p *FTPImporter) Close(ctx context.Context) error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

func (p *FTPImporter) Root() string {
	return p.rootDir
}

func (p *FTPImporter) Origin() string {
	return p.host
}

func (p *FTPImporter) Type() string {
	return "ftp"
}

func (p *FTPImporter) Flags() location.Flags {
	return 0
}
