package sysone

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveImage converts an image reference into a data URL.
//
// Data URLs (scheme data:) pass through unchanged. Anything else is treated
// as a local file path: the file is read, size-capped at maxBytes, and
// base64-encoded with a MIME type derived from the extension.
func resolveImage(ctx context.Context, ref string, maxBytes int64) (string, error) {
	if strings.HasPrefix(ref, "data:") {
		return ref, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", invalid(fmt.Sprintf("image %q: %v", ref, err),
			"check the file path, or pass a data: URL to embed image bytes directly")
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return "", invalid(fmt.Sprintf("image %q is %d bytes, over the limit of %d bytes", ref, len(data), maxBytes),
			"shrink the image, or raise Config.MaxImageBytes")
	}
	mime := mimeFromExt(filepath.Ext(ref))
	if mime == "" {
		return "", invalid(fmt.Sprintf("image %q: unknown image type %q", ref, filepath.Ext(ref)),
			"use one of .png .jpg .jpeg .webp .gif, or pass a data: URL")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// mimeFromExt maps a lowercase file extension to an image MIME type,
// returning "" for unknown extensions.
func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}
