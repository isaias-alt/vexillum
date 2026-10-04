package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// verifyChecksum checks data against the sha256 that checksums (the release's
// checksums.txt, "<hex>  <file name>" per line) lists for name. A file that is
// not listed is a failure too: nothing unverified gets installed.
func verifyChecksum(checksums []byte, name string, data []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s is not listed in checksums.txt - refusing to install an unverified binary", name)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", name, want, got)
	}
	return nil
}

// extractBinary returns the contents of the regular file called name inside a
// .tar.gz archive, wherever in the archive it sits.
func extractBinary(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("opening the archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s is not in the archive", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != name {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxAssetBytes+1))
		if err != nil {
			return nil, fmt.Errorf("reading %s from the archive: %w", name, err)
		}
		if len(data) > maxAssetBytes {
			return nil, fmt.Errorf("%s in the archive is larger than %d bytes", name, maxAssetBytes)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("%s in the archive is empty", name)
		}
		return data, nil
	}
}
