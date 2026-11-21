package protocol

// Response is an HTTP response under construction. Instances are pooled by the
// server, so handlers must not retain them.
type Response struct {
	StatusCode int
	Header     Header
	body       []byte
}

// Body returns the response body written so far.
func (r *Response) Body() []byte { return r.body }

// SetBody replaces the body. The slice is not copied and must not be modified
// until the response has been written.
func (r *Response) SetBody(body []byte) { r.body = body }

// SetBodyString replaces the body with a copy of s.
func (r *Response) SetBodyString(s string) { r.body = append(r.body[:0], s...) }

// AppendBody appends p to the body.
func (r *Response) AppendBody(p []byte) { r.body = append(r.body, p...) }

// Reset restores the defaults (status 200, no headers, empty body).
func (r *Response) Reset() {
	r.StatusCode = 200
	r.Header.Reset()
	r.body = r.body[:0]
}
