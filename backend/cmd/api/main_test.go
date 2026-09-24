package main

import "testing"

func TestParseDatabaseSSL(t *testing.T) {
	cases := []struct {
		value string
		want  bool
		fail  bool
	}{
		{value: "", want: false},
		{value: "false", want: false},
		{value: "off", want: false},
		{value: "TRUE", want: true},
		{value: "require", want: true},
		{value: "unexpected", fail: true},
	}
	for _, tc := range cases {
		got, err := parseDatabaseSSL(tc.value)
		if (err != nil) != tc.fail || (!tc.fail && got != tc.want) {
			t.Fatalf("parseDatabaseSSL(%q) = %v, %v; want %v, fail=%v", tc.value, got, err, tc.want, tc.fail)
		}
	}
}
