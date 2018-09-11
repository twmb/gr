package g

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	bs, err := ioutil.ReadFile("../cmd/gr/goro")
	if err != nil {
		b.Fatal(err)
	}
	r := bytes.NewReader(bs)
	o := *r
	for i := 0; i < b.N; i++ {
		Parse(r)
		*r = o
	}
}
