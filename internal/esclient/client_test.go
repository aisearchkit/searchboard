package esclient

import "testing"

func TestVersionCapabilities(t *testing.T) {
	cases := []struct {
		f       Flavor
		v       string
		pit, sa bool
	}{
		{FlavorElasticsearch, "5.6.16", false, false},
		{FlavorElasticsearch, "6.8.0", false, true},
		{FlavorElasticsearch, "7.9.3", false, true},
		{FlavorElasticsearch, "7.10.0", true, true},
		{FlavorElasticsearch, "8.15.0", true, true},
		{FlavorOpenSearch, "2.3.0", false, true},
		{FlavorOpenSearch, "2.4.0", true, true},
	}
	for _, c := range cases {
		v := ParseVersion(c.f, c.v)
		if v.SupportsPIT() != c.pit || v.SupportsSearchAfter() != c.sa {
			t.Errorf("%s %s: pit=%v sa=%v", c.f, c.v, v.SupportsPIT(), v.SupportsSearchAfter())
		}
	}
}
