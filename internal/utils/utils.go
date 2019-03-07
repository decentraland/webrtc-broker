package utils

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"time"
)

func NowMs() float64 {
	return Milliseconds(time.Now())
}

func Milliseconds(t time.Time) float64 {
	return float64(t.UnixNano() / int64(time.Millisecond))
}

func Max(a int, b int) int {
	if a > b {
		return a
	}

	return b
}

func Min(a int, b int) int {
	if a < b {
		return a
	}

	return b
}

type ZipCompression interface {
	Zip(plain []byte) ([]byte, error)
	Unzip(zipped []byte) ([]byte, error)
}

type GzipCompression struct{}

func (g *GzipCompression) Zip(plain []byte) ([]byte, error) {
	var b bytes.Buffer

	gz := gzip.NewWriter(&b)

	if _, err := gz.Write(plain); err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (g *GzipCompression) Unzip(zipped []byte) ([]byte, error) {
	b := bytes.NewBuffer(zipped)

	gz, err := gzip.NewReader(b)

	if err != nil {
		return nil, err
	}

	r, err := ioutil.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return r, nil
}
