// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrds3 implements a minimal S3 client (AWS Signature Version 4) for
// the XRootD S3 backend: object GET (with ranges), HEAD, PUT and DELETE.
// Credentials are sourced via go-hep.org/x/hep/xrootd/internal/s3cred.
package xrds3 // import "go-hep.org/x/hep/xrootd/xrds3"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is the SHA-256 of an empty body, used for requests (GET,
// HEAD, DELETE) whose payload is empty.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// unsignedPayload marks a request body as not included in the signature; the
// server accepts it when the header x-amz-content-sha256 is set to this value.
const unsignedPayload = "UNSIGNED-PAYLOAD"

func sha256hex(p []byte) string {
	sum := sha256.Sum256(p)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// signingKey derives the SigV4 signing key for a date/region/service.
func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// uriEncodePath percent-encodes an S3 object key path per SigV4 rules: every
// byte except unreserved characters and '/' is encoded.
func uriEncodePath(p string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~/"
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hexd = "0123456789ABCDEF"
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&0xf])
		}
	}
	return b.String()
}

// canonicalQuery builds the canonical query string: sorted, percent-encoded.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// sign applies an AWS SigV4 Authorization header (and the required
// x-amz-date / x-amz-content-sha256 headers) to req for the given credentials.
// payloadHash is the hex SHA-256 of the body, or unsignedPayload.
func sign(req *http.Request, accessKey, secret, region, service, payloadHash string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if req.Host == "" {
		req.Host = req.URL.Host
	}
	req.Header.Set("host", req.URL.Host)

	// Canonical headers: host, x-amz-content-sha256, x-amz-date (sorted).
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"

	canonicalRequest := strings.Join([]string{
		req.Method,
		uriEncodePath(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256hex([]byte(canonicalRequest)),
	}, "\n")

	sig := hex.EncodeToString(hmacSHA256(signingKey(secret, dateStamp, region, service), []byte(stringToSign)))

	auth := "AWS4-HMAC-SHA256 " +
		"Credential=" + accessKey + "/" + scope + ", " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=" + sig
	req.Header.Set("Authorization", auth)
}
