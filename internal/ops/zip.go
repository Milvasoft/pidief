package ops

import (
	"archive/zip"
	"bytes"
	"time"
)

// Zip packs files into a ZIP archive (stored names are de-duplicated by the caller's naming).
func Zip(files []File) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: time.Now()}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
