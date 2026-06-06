package db

import "crypto/tls"

var tlsRejectUnauthorizedFalse = tls.Config{InsecureSkipVerify: true} //nolint:gosec
