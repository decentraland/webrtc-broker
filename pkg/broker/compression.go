package broker

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
)

// ZipCompression is the zip compression interface
type ZipCompression interface {
	Zip(plain []byte) ([]byte, error)
	Unzip(zipped []byte) ([]byte, error)
}

// GzipCompression compressor for gzip format
type GzipCompression struct{}

// Zip the given byte array
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

// Unzip the given byte array
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
