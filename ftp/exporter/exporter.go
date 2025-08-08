/*
 * Copyright (c) 2025 Gilles Chehade <gilles@plakar.io>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package exporter

import (
	"context"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/PlakarKorp/integration-ftp/common"
	"github.com/PlakarKorp/kloset/objects"
	"github.com/PlakarKorp/kloset/snapshot/exporter"
	"github.com/secsy/goftp"
)

type FTPExporter struct {
	host    string
	rootDir string
	client  *goftp.Client
}

func NewFTPExporter(ctx context.Context, opts *exporter.Options, name string, config map[string]string) (exporter.Exporter, error) {
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

	client, err := common.ConnectToFTP(parsed.Host, username, password)
	if err != nil {
		return nil, err
	}

	return &FTPExporter{
		host:    parsed.Host,
		rootDir: parsed.Path,
		client:  client,
	}, nil
}

func (p *FTPExporter) Root(ctx context.Context) (string, error) {
	return p.rootDir, nil
}

func (p *FTPExporter) CreateDirectory(ctx context.Context, pathname string) error {
	if pathname == "/" || pathname == "." {
		return nil
	}

	pathname = strings.TrimSuffix(path.Clean(pathname), "/")
	parts := strings.Split(pathname, "/")

	// start from absolute or relative root
	currPath := ""
	if strings.HasPrefix(pathname, "/") {
		currPath = "/"
	}

	for _, part := range parts {
		if part == "" {
			continue
		}
		currPath = path.Join(currPath, part)

		_, err := p.client.Mkdir(currPath)
		if err != nil {
			// 550 = already exists OR cannot create
			if strings.Contains(err.Error(), "550") ||
				strings.Contains(err.Error(), "exists") ||
				strings.Contains(err.Error(), "File exists") ||
				strings.Contains(err.Error(), "File exists") {
				continue
			}
			return err
		}
	}

	return nil
}

func (p *FTPExporter) StoreFile(ctx context.Context, pathname string, fp io.Reader, size int64) error {
	return p.client.Store(pathname, fp)
}

func (p *FTPExporter) SetPermissions(ctx context.Context, pathname string, fileinfo *objects.FileInfo) error {
	// can't chown/chmod on FTP
	return nil
}

func (p *FTPExporter) CreateLink(ctx context.Context, oldname string, newname string, ltype exporter.LinkType) error {
	return errors.ErrUnsupported
}

func (p *FTPExporter) Close(ctx context.Context) error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
