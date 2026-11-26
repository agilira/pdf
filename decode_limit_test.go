package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// bomb is a PDF of pages that all show one stream decoding to size bytes.
func bomb(t *testing.T, pages, size int) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	chunk := []byte(strings.Repeat("0 0 m ", 1024))
	for n := 0; n < size; n += len(chunk) {
		_, _ = zw.Write(chunk)
	}
	_ = zw.Close()
	var b bytes.Buffer
	offsets := []int{}
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	kids := []string{}
	for i := 0; i < pages; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+i))
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages))
	offsets = append(offsets, b.Len())
	fmt.Fprintf(&b, "3 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", z.Len())
	b.Write(z.Bytes())
	b.WriteString("\nendstream\nendobj\n")
	for i := 0; i < pages; i++ {
		obj("<< /Type /Page /Parent 2 0 R /Contents 3 0 R /Resources << >> >>")
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// A small file whose pages all decode one large stream stops at the
// limit, quickly; every stream after fails at once.
func TestDecodeLimit(t *testing.T) {
	data := bomb(t, 50, 8<<20)
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	r.SetDecodeLimit(16 << 20)
	start := time.Now()
	failed := 0
	for n := 1; n <= r.NumPage(); n++ {
		if _, err := r.Page(n).GetPlainText(nil); err != nil {
			failed++
		}
	}
	if failed < 45 || r.decoded.Load() > 16<<20 {
		t.Fatalf("%d pages failed, %d bytes decoded", failed, r.decoded.Load())
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Fatalf("took %v", took)
	}
	rd := r.Page(1).V.Key("Contents").Reader()
	if _, err := rd.Read(make([]byte, 10)); !errors.Is(err, ErrDecodeLimit) {
		t.Fatalf("after the limit: %v", err)
	}
}

// Without a limit nothing changes.
func TestNoDecodeLimit(t *testing.T) {
	data := bomb(t, 2, 1<<20)
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Page(1).GetPlainText(nil); err != nil {
		t.Fatal(err)
	}
}

// A stream that decodes to exactly the limit is read whole.
func TestDecodeLimitExact(t *testing.T) {
	data := bomb(t, 1, 6144)
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	r.SetDecodeLimit(6144)
	if _, err := r.Page(1).GetPlainText(nil); err != nil {
		t.Fatal(err)
	}
}
