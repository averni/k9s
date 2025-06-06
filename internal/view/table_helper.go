// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/config/data"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/slogs"
	"github.com/derailed/k9s/internal/ui"
)

func computeFilename(dumpPath, ns, title, path string) (string, error) {
	now := time.Now().UnixNano()

	dir := dumpPath
	if err := ensureDir(dir); err != nil {
		return "", err
	}

	name := title + "-" + data.SanitizeFileName(path)
	if path == "" {
		name = title
	}

	var fName string
	if ns == client.ClusterScope {
		fName = fmt.Sprintf(ui.NoNSFmat, name, now)
	} else {
		fName = fmt.Sprintf(ui.FullFmat, name, ns, now)
	}

	return strings.ToLower(filepath.Join(dir, fName)), nil
}

func saveTable(dir, title, path string, mdata *model1.TableData) (string, error) {
	ns := mdata.GetNamespace()
	if client.IsClusterWide(ns) {
		ns = client.NamespaceAll
	}

	fPath, err := computeFilename(dir, ns, title, path)
	if err != nil {
		return "", err
	}
	slog.Debug("Saving table to disk", slogs.FileName, fPath)

	mod := os.O_CREATE | os.O_WRONLY
	out, err := os.OpenFile(fPath, mod, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := out.Close(); err != nil {
			slog.Error("Closing file failed",
				slogs.Path, fPath,
				slogs.Error, err,
			)
		}
	}()

	w := csv.NewWriter(out)
	_ = w.Write(mdata.ColumnNames(true))

	mdata.RowsRange(func(_ int, re model1.RowEvent) bool {
		_ = w.Write(re.Row.Fields)
		return true
	})
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}

	return fPath, nil
}
