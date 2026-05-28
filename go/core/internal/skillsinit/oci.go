package skillsinit

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
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

// extractTar writes the tar stream into dst. All filesystem operations go
// through an os.Root anchored at dst, so any path or symlink that would
// resolve outside dst is rejected by the kernel.
func extractTar(r io.Reader, dst string) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open root %s: %w", dst, err)
	}
	defer root.Close()

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := tarEntryToLocal(hdr.Name)
		if err != nil {
			return fmt.Errorf("tar entry %q: %w", hdr.Name, err)
		}
		if rel == "" {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(rel, os.FileMode(hdr.Mode)|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			// OCI layers can overwrite read-only files from earlier layers.
			// Removing first avoids EACCES when O_TRUNC would otherwise fail.
			_ = root.Remove(rel)
			f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			if err := validateSymlinkTarget(hdr.Name, rel, hdr.Linkname); err != nil {
				return err
			}
			_ = root.Remove(rel)
			if err := root.Symlink(hdr.Linkname, rel); err != nil {
				return err
			}
		default:
			// Skip hardlinks, devices, etc. Not meaningful in a skill bundle.
		}
	}
}

// tarEntryToLocal converts a tar header name (always slash-separated, may
// have a leading "/") into a local OS path that's guaranteed to stay inside
// the root. Returns "" for the no-op "." / "" entries. Delegates the actual
// safety check to filepath.Localize, which rejects ".." segments and any
// path that can't be a relative local path.
func tarEntryToLocal(name string) (string, error) {
	// Strip leading "/" — tar convention for "absolute" entries is to re-root
	// them under the destination, not to escape. Strip trailing "/" too since
	// tar directory entries carry one and filepath.Localize rejects it.
	// filepath.Localize then catches ".." traversal and anything else not
	// locally representable.
	trimmed := strings.Trim(name, "/")
	if trimmed == "" || trimmed == "." {
		return "", nil
	}
	local, err := filepath.Localize(trimmed)
	if err != nil {
		return "", fmt.Errorf("escapes destination: %w", err)
	}
	return local, nil
}

// validateSymlinkTarget rejects symlinks whose target would resolve outside
// the root. os.Root.Symlink itself only creates the link verbatim — it does
// not enforce that the target stays in-root — so we have to check here.
func validateSymlinkTarget(entryName, linkPath, linkTarget string) error {
	if filepath.IsAbs(linkTarget) {
		return fmt.Errorf("tar entry %q has absolute symlink target %q", entryName, linkTarget)
	}
	// Resolve link target relative to the link's *directory*. We use slash-
	// based path.Join + path.Clean because tar names are slash-separated and
	// the result is then validated as a single relative path.
	resolved := path.Join(filepath.ToSlash(filepath.Dir(linkPath)), filepath.ToSlash(linkTarget))
	if resolved == "" || resolved == "." {
		return nil
	}
	if !filepath.IsLocal(filepath.FromSlash(resolved)) {
		return fmt.Errorf("tar entry %q symlink target %q escapes destination", entryName, linkTarget)
	}
	return nil
}
