package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const signatureQuery = "signature"

type URLSigner struct {
	secret []byte
	now    func() time.Time
}

func NewURLSigner(secret []byte) (*URLSigner, error) {
	if len(secret) < 32 {
		return nil, errors.New("media URL signing secret must contain at least 32 bytes")
	}
	return &URLSigner{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (signer *URLSigner) Sign(method string, path string, values url.Values, expiresAt time.Time) string {
	copyValues := cloneValues(values)
	copyValues.Del(signatureQuery)
	copyValues.Set("expires", strconv.FormatInt(expiresAt.UTC().Unix(), 10))
	copyValues.Set(signatureQuery, signer.signature(method, path, copyValues))
	return path + "?" + copyValues.Encode()
}

func (signer *URLSigner) Verify(method string, path string, values url.Values) bool {
	if signer == nil {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimSpace(values.Get(signatureQuery)))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	expiresUnix, err := strconv.ParseInt(values.Get("expires"), 10, 64)
	if err != nil || !time.Unix(expiresUnix, 0).After(signer.now()) {
		return false
	}
	copyValues := cloneValues(values)
	copyValues.Del(signatureQuery)
	expected, _ := hex.DecodeString(signer.signature(method, path, copyValues))
	return hmac.Equal(provided, expected)
}

func (signer *URLSigner) signature(method string, path string, values url.Values) string {
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte(strings.ToUpper(strings.TrimSpace(method))))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(path))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(values.Encode()))
	return hex.EncodeToString(mac.Sum(nil))
}

func cloneValues(values url.Values) url.Values {
	out := make(url.Values, len(values))
	for key, entries := range values {
		out[key] = append([]string(nil), entries...)
	}
	return out
}
