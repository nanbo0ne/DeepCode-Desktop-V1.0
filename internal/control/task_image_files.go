package control

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func taskImageRelativePath(workspace, path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "\x00\r\n") {
		return "", fmt.Errorf("invalid image path")
	}
	abs, base, ok := resolveAbsRef(path, workspace)
	if !ok || base == "" {
		return "", fmt.Errorf("image path is outside the workspace")
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || !filepath.IsLocal(rel) || strings.Contains(rel, ":") {
		return "", fmt.Errorf("invalid workspace image path")
	}
	return rel, nil
}

func openTaskImageRoot(workspace string) (*os.Root, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(abs)
	if strings.HasPrefix(volume, `\\`) {
		return nil, fmt.Errorf("image workspace must be on a local filesystem")
	}
	anchor := volume + string(filepath.Separator)
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(anchor, abs)
	if err != nil {
		root.Close()
		return nil, err
	}
	// Open each component from an already pinned directory handle. Checking an
	// absolute path and then reopening it would race an ancestor junction swap.
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "." {
			continue
		}
		child, err := openTaskImageDir(root, part)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = child
	}
	return root, nil
}

func openTaskImageDir(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if taskImageLink(before) || !before.IsDir() {
		return nil, fmt.Errorf("image directory must not be a link or reparse point")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	after, afterErr := parent.Lstat(name)
	if err != nil || afterErr != nil || taskImageLink(after) || !os.SameFile(before, opened) || !os.SameFile(before, after) {
		child.Close()
		return nil, fmt.Errorf("image directory changed while opening")
	}
	return child, nil
}

func taskImagePathInfo(root *os.Root, rel string) (os.FileInfo, error) {
	var info os.FileInfo
	path := "."
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		path = filepath.Join(path, part)
		var err error
		info, err = root.Lstat(path)
		if err != nil {
			return nil, err
		}
		if taskImageLink(info) {
			return nil, fmt.Errorf("image path must not contain links or reparse points")
		}
	}
	return info, nil
}

func readTaskImage(root *os.Root, rel string) ([]byte, error) {
	before, err := taskImagePathInfo(root, rel)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxImageAttachmentBytes {
		return nil, fmt.Errorf("image must be a regular file between 1 byte and 10 MB")
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("image changed while opening")
	}
	if err := checkTaskImageHandle(f, opened); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return nil, fmt.Errorf("image changed while reading")
	}
	current, err := taskImagePathInfo(root, rel)
	if err != nil || !os.SameFile(opened, current) {
		return nil, fmt.Errorf("image path changed while reading")
	}
	return raw, nil
}

func validateTaskImage(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("image must be between 1 byte and 10 MB")
	}
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		if err := validateTaskWebPHeaders(raw); err != nil {
			return "", err
		}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif" && format != "webp") {
		return "", fmt.Errorf("task images require valid PNG, JPEG, GIF or WebP bytes")
	}
	if err := validateTaskImageDimensions(cfg.Width, cfg.Height); err != nil {
		return "", err
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("invalid image data: %w", err)
	}
	if decodedFormat != format || (format != "gif" && (decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height)) {
		return "", fmt.Errorf("decoded image does not match its validated header")
	}
	return "image/" + format, nil
}

func validateTaskImageDimensions(width, height int) error {
	const maxPixels = 32 * 1024 * 1024
	if width <= 0 || height <= 0 || width > maxPixels/height {
		return fmt.Errorf("image dimensions exceed the 32 megapixel limit")
	}
	return nil
}

func snapshotTaskImage(root *os.Root, ext string, raw []byte) (string, error) {
	parent := root
	for _, dir := range []string{".orca", "attachments"} {
		if err := parent.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
			return "", err
		}
		child, err := openTaskImageDir(parent, dir)
		if err != nil {
			return "", err
		}
		defer child.Close()
		parent = child
	}
	for range maxAttachmentCreateAttempts {
		path := attachmentPath(ext)
		f, err := parent.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if _, err := taskImagePathInfo(root, path); err != nil {
			return "", err
		}
		return filepath.ToSlash(path), nil
	}
	return "", fmt.Errorf("could not create image snapshot")
}
