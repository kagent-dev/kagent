package skillsinit

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// FetchOCI pulls the named image, exports its flattened filesystem, and
// extracts it into ref.Dest. It is the in-process replacement for the old
// `krane export | tar xf -` pipeline.
//
// Auth comes from the standard DOCKER_CONFIG mechanism (set by the caller
// after MergeDockerConfigs). Platform follows the host arch — same as the
// old script's case statement on `uname -m`.
func FetchOCI(ref OCIRef, insecure bool) error {
	platform, err := hostPlatform()
	if err != nil {
		return err
	}

	opts := []crane.Option{crane.WithPlatform(platform)}
	if insecure {
		opts = append(opts, crane.Insecure)
	}

	img, err := crane.Pull(ref.Image, opts...)
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref.Image, err)
	}

	if err := os.MkdirAll(ref.Dest, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", ref.Dest, err)
	}

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		exportErr := crane.Export(img, pw)
		_ = pw.CloseWithError(exportErr)
		errCh <- exportErr
	}()

	if err := extractTar(pr, ref.Dest); err != nil {
		// Abort the export promptly; don't drain potentially large images.
		_ = pr.CloseWithError(err)
		<-errCh
		return fmt.Errorf("extract %s: %w", ref.Image, err)
	}
	if err := <-errCh; err != nil {
		return fmt.Errorf("export %s: %w", ref.Image, err)
	}
	return nil
}

func hostPlatform() (*v1.Platform, error) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture for OCI export: %s", runtime.GOARCH)
	}
	return &v1.Platform{OS: "linux", Architecture: arch}, nil
}

// extractTar writes the tar stream into dst. We refuse entries that escape
// dst via absolute paths or ".." segments — the old `tar xf` accepted those,
// and untrusted skill images are exactly the case where that's dangerous.
func extractTar(r io.Reader, dst string) error {
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dstAbs, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// OCI layers can overwrite read-only files from earlier layers.
			// Removing first avoids EACCES when O_TRUNC would otherwise fail.
			_ = os.Remove(target)
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Reject symlinks whose target escapes dst.
			linkTarget := hdr.Linkname
			if filepath.IsAbs(linkTarget) {
				return fmt.Errorf("tar entry %q has absolute symlink target %q", hdr.Name, linkTarget)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkTarget))
			if !strings.HasPrefix(resolved+string(os.PathSeparator), dstAbs+string(os.PathSeparator)) && resolved != dstAbs {
				return fmt.Errorf("tar entry %q symlink target %q escapes destination", hdr.Name, linkTarget)
			}
			_ = os.Remove(target)
			if err := os.Symlink(linkTarget, target); err != nil {
				return err
			}
		default:
			// Skip hardlinks, devices, etc. Not meaningful in a skill bundle.
		}
	}
}

func safeJoin(dst, name string) (string, error) {
	// Ensure the tar entry path is treated as relative so filepath.Join doesn't
	// discard dst. We still validate after joining to prevent escapes via "..".
	cleaned := filepath.Clean(name)
	cleaned = strings.TrimPrefix(cleaned, string(os.PathSeparator))
	if cleaned == "." {
		return dst, nil
	}
	target := filepath.Join(dst, cleaned)
	if !strings.HasPrefix(target+string(os.PathSeparator), dst+string(os.PathSeparator)) && target != dst {
		return "", fmt.Errorf("tar entry %q escapes destination", name)
	}
	return target, nil
}
