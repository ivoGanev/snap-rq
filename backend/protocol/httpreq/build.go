// Package httpreq turns a stored HTTP request into the request that goes
// out. Sending, trying a script and copying as cURL all go through it, so
// they agree on what is sent. Other protocols get a package of their own
// next to it (backend/protocol).
//
// It is two steps because scripts sit between them: Resolve fills in
// {{variables}}, a beforeRequest script may edit the result (and write new
// placeholders, so Resolve runs again), then Build makes it ready to send.
package httpreq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"snap-rq/backend/models"
)

// Resolve fills in the environment's values in the URL, headers, body and
// form-data text fields; file paths are left as they are. It returns the
// locked secrets it met (see models.Environment.Fill).
func Resolve(req models.HttpRequest, env models.Environment) (models.HttpRequest, []string) {
	var locked []string
	fill := func(text string) string {
		filled, more := env.Fill(text)
		locked = append(locked, more...)
		return filled
	}
	req.URL = fill(req.URL)
	req.RequestHeaders = fill(req.RequestHeaders)
	req.Body = fill(req.Body)
	if strings.Contains(req.BodyForm, "{{") {
		if fields, err := models.ParseFormFields(req.BodyForm); err == nil {
			for i := range fields {
				if fields[i].Type != "file" {
					fields[i].Value = fill(fields[i].Value)
				}
			}
			if out, err := json.Marshal(fields); err == nil {
				req.BodyForm = string(out)
			}
		}
	}
	return req, locked
}

// Outgoing is a request as it goes out.
type Outgoing struct {
	Method string
	URL    string
	Header http.Header
	// Body is a JSON or text body; Form the enabled fields of a form-data one.
	Body     string
	FormData bool
	Form     []models.FormField
}

// Build makes a resolved request ready to send: the method, a scheme for
// the URL, checked header lines, and the body with its Content-Type. On an
// error, Method and URL are still set, so a failed send is saved with them.
func Build(req models.HttpRequest) (Outgoing, error) {
	out := Outgoing{Method: strings.ToUpper(strings.TrimSpace(req.Method)), URL: withScheme(req.URL)}
	if out.Method == "" {
		out.Method = http.MethodGet
	}

	header, err := parseHeaders(req.RequestHeaders)
	if err != nil {
		return out, err
	}
	out.Header = header

	switch req.BodyType {
	case "form-data":
		out.FormData = true
		fields, err := models.ParseFormFields(req.BodyForm)
		if err != nil {
			return out, fmt.Errorf("parsing form body: %w", err)
		}
		for _, f := range fields {
			if f.Enabled && f.Key != "" {
				out.Form = append(out.Form, f)
			}
		}
	default:
		out.Body = req.Body
		if out.Body != "" && header.Get("Content-Type") == "" {
			contentType := "application/json"
			if req.BodyType == "text" {
				contentType = "text/plain"
			}
			header.Set("Content-Type", contentType)
		}
	}
	return out, nil
}

// HTTPRequest is the request for the HTTP client. A form-data body is
// encoded here, so files are read only when actually sending.
func (o Outgoing) HTTPRequest(ctx context.Context) (*http.Request, error) {
	var body io.Reader
	header := o.Header.Clone()
	if o.FormData {
		encoded, contentType, err := encodeForm(o.Form)
		if err != nil {
			return nil, err
		}
		body = encoded
		if header.Get("Content-Type") == "" {
			header.Set("Content-Type", contentType)
		}
	} else if o.Body != "" {
		body = strings.NewReader(o.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, o.Method, o.URL, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %v", err)
	}
	httpReq.Header = header
	return httpReq, nil
}

// withScheme prepends http:// when a URL has none, so URLs typed by hand or
// imported without one still send.
func withScheme(u string) string {
	if u == "" || strings.Contains(u, "://") {
		return u
	}
	return "http://" + u
}

// encodeForm writes the fields as a multipart body. Files are opened from
// their local paths, never read into the database.
func encodeForm(fields []models.FormField) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.Type == "file" {
			if err := writeFile(writer, f); err != nil {
				return nil, "", err
			}
			continue
		}
		if err := writer.WriteField(f.Key, f.Value); err != nil {
			return nil, "", fmt.Errorf("writing field %s: %w", f.Key, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func writeFile(writer *multipart.Writer, f models.FormField) error {
	filename := f.FileName
	if filename == "" {
		filename = filepath.Base(f.FilePath)
	}
	part, err := writer.CreateFormFile(f.Key, filename)
	if err != nil {
		return fmt.Errorf("creating file part %s: %w", f.Key, err)
	}
	file, err := os.Open(f.FilePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s", WithDetails(
			fmt.Sprintf("The file %s for the form field %s can't be found.", filename, f.Key),
			fmt.Sprintf("opening file %s: %v", f.FilePath, err)))
	}
	if err != nil {
		return fmt.Errorf("opening file %s: %w", f.FilePath, err)
	}
	defer file.Close()
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("reading file %s: %w", f.FilePath, err)
	}
	return nil
}
