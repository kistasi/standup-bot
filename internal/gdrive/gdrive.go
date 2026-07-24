// Package gdrive backs up finished reports to Google Drive. It reuses the same
// Google credentials JSON as internal/gcal — typically authorized-user
// credentials from `gcloud auth application-default login` — but needs the
// drive.file scope added to them.
//
// drive.file is deliberate: it is a non-sensitive scope that grants access only
// to files the app itself created, so it needs no Workspace admin grant and no
// app verification, and the bot can never read the rest of the user's Drive.
// The flip side is that a folder created by hand in the Drive UI is invisible
// here, which is why the folder is named rather than identified by ID: the
// client looks for its own folder of that name and creates it on first run.
package gdrive

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// mimeType is what reports are stored as. Both the standup summary and the
// monthly report are markdown.
const mimeType = "text/markdown"

// folderMIME is Drive's marker type for a folder.
const folderMIME = "application/vnd.google-apps.folder"

// Client uploads reports into one Drive folder owned by the bot.
type Client struct {
	svc        *drive.Service
	folderName string

	// folderID is resolved lazily on the first upload and cached, so a run that
	// uploads twice creates the folder at most once.
	folderID string
}

// New builds a Client from a Google credentials JSON (authorized-user or
// service-account). folderName is the My Drive folder reports are written to;
// it is created on first upload if the app has not created it already.
//
// A service-account key works but is rarely what you want: the files land in
// the service account's own Drive, which no human can browse.
func New(ctx context.Context, credsJSON []byte, folderName string) (*Client, error) {
	svc, err := drive.NewService(ctx,
		option.WithCredentialsJSON(credsJSON),
		option.WithScopes(drive.DriveFileScope),
	)
	if err != nil {
		return nil, fmt.Errorf("gdrive: %w", err)
	}
	return &Client{svc: svc, folderName: folderName}, nil
}

// Upload writes content to Drive as name inside the configured folder and
// returns a link to the file. Re-uploading the same name updates the existing
// file in place rather than adding a duplicate, so re-running a report keeps
// one file (and one shareable link) per period — matching how monthly.Write
// overwrites its local copy.
func (c *Client) Upload(ctx context.Context, name string, content []byte) (string, error) {
	folderID, err := c.ensureFolder(ctx)
	if err != nil {
		return "", err
	}

	existing, err := c.findFile(ctx, name, folderID)
	if err != nil {
		return "", err
	}

	media := googleapi.ChunkSize(0) // small files: one request, no resumable session
	if existing != "" {
		// An empty (not nil) body: the metadata is unchanged, only the bytes are
		// replaced, but the generated client still marshals the body it is given.
		file, err := c.svc.Files.Update(existing, &drive.File{}).
			Media(bytes.NewReader(content), googleapi.ContentType(mimeType), media).
			Fields("webViewLink").
			Context(ctx).Do()
		if err != nil {
			return "", fmt.Errorf("gdrive: updating %q: %w", name, err)
		}
		return file.WebViewLink, nil
	}

	file, err := c.svc.Files.Create(&drive.File{
		Name:     name,
		Parents:  []string{folderID},
		MimeType: mimeType,
	}).
		Media(bytes.NewReader(content), googleapi.ContentType(mimeType), media).
		Fields("webViewLink").
		Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: creating %q: %w", name, err)
	}
	return file.WebViewLink, nil
}

// ensureFolder returns the ID of the reports folder, creating it if this app
// has not created it before. Under drive.file the lookup only ever sees the
// app's own files, so a same-named folder made by hand in the UI is not found
// (and not reused) by design.
func (c *Client) ensureFolder(ctx context.Context) (string, error) {
	if c.folderID != "" {
		return c.folderID, nil
	}

	q := fmt.Sprintf("name = '%s' and mimeType = '%s' and trashed = false", escape(c.folderName), folderMIME)
	list, err := c.svc.Files.List().Q(q).Fields("files(id)").PageSize(1).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: looking up folder %q: %w", c.folderName, err)
	}
	if len(list.Files) > 0 {
		c.folderID = list.Files[0].Id
		return c.folderID, nil
	}

	folder, err := c.svc.Files.Create(&drive.File{
		Name:     c.folderName,
		MimeType: folderMIME,
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: creating folder %q: %w", c.folderName, err)
	}
	c.folderID = folder.Id
	return c.folderID, nil
}

// findFile returns the ID of a file called name in folderID, or "" when the app
// has not created one.
func (c *Client) findFile(ctx context.Context, name, folderID string) (string, error) {
	q := fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", escape(name), folderID)
	list, err := c.svc.Files.List().Q(q).Fields("files(id)").PageSize(1).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: looking up %q: %w", name, err)
	}
	if len(list.Files) == 0 {
		return "", nil
	}
	return list.Files[0].Id, nil
}

// escape quotes a value for a Drive query string, where the delimiter is a
// single quote and backslash is the escape character.
func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}
