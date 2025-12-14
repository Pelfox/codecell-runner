package pkg

import (
	"archive/tar"
	"bytes"
	"io"
)

// CreateTar creates a new tar archive for submitted files and their byte representation.
func CreateTar(files map[string][]byte) (io.Reader, error) {
	buffer := new(bytes.Buffer)
	tarWriter := tar.NewWriter(buffer)
	defer tarWriter.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(content); err != nil {
			return nil, err
		}
	}

	return buffer, nil
}
