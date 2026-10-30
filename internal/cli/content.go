package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// maxContentBytes bounds the content read by --file and --append-file.
const maxContentBytes = 10 << 20

// readContent reads the Markdown given to flag: the file at path, or stdin
// when path is "-". The content must be valid UTF-8, at most 10 MiB and not
// blank; any failure is a usage error naming the flag.
func readContent(flag, path string, stdin io.Reader) (string, error) {
	invalid := func(reason string) error {
		return usageErrorf("invalid %s %q: %s", flag, path, reason)
	}
	src := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return "", invalid("cannot read: " + pathErrorReason(err))
		}
		defer f.Close()
		src = f
	}
	data, err := io.ReadAll(io.LimitReader(src, maxContentBytes+1))
	if err != nil {
		return "", invalid("cannot read: " + pathErrorReason(err))
	}
	switch {
	case len(data) > maxContentBytes:
		return "", invalid("larger than 10 MiB")
	case !utf8.Valid(data):
		return "", invalid("not valid UTF-8")
	case len(bytes.TrimSpace(data)) == 0:
		return "", invalid("empty or only whitespace")
	}
	return string(data), nil
}

// pathErrorReason drops the path from err, already shown by the caller.
func pathErrorReason(err error) string {
	var perr *fs.PathError
	if errors.As(err, &perr) {
		return perr.Err.Error()
	}
	return err.Error()
}

// markdownToHTML converts Markdown the way it is stored in a WYSIWYG page:
// GFM (tables, strikethrough, task lists, autolinks) with raw HTML passed
// through. BookStack sanitizes the HTML on save.
var markdownToHTML = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

func convertMarkdown(md string) (string, error) {
	var buf strings.Builder
	if err := markdownToHTML.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
