package esclient

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		raw     string
		want    [3]int
		wantErr bool
	}{
		{raw: "7.10.2", want: [3]int{7, 10, 2}},
		{raw: "8.0.0-rc1", want: [3]int{8, 0, 0}},
		{raw: "2.11", want: [3]int{2, 11, 0}},
		{raw: " 5.6.16 ", want: [3]int{5, 6, 16}},
		{raw: "", wantErr: true},
		{raw: "seven", wantErr: true},
		{raw: "7.x.1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseVersion(FlavorElasticsearch, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Major != tt.want[0] || got.Minor != tt.want[1] || got.Patch != tt.want[2] {
				t.Fatalf("got %d.%d.%d, want %v", got.Major, got.Minor, got.Patch, tt.want)
			}
		})
	}
}

func TestVersionCapabilities(t *testing.T) {
	tests := []struct {
		flavor         Flavor
		raw            string
		pit            bool
		searchAfter    bool
		removedTypes   bool
		clusterManager bool
	}{
		{FlavorElasticsearch, "5.6.16", false, false, false, false},
		{FlavorElasticsearch, "6.8.0", false, true, false, false},
		{FlavorElasticsearch, "7.9.3", false, true, false, false},
		{FlavorElasticsearch, "7.10.0", true, true, false, false},
		{FlavorElasticsearch, "8.15.0", true, true, true, false},
		{FlavorElasticsearch, "9.0.0", true, true, true, false},
		{FlavorOpenSearch, "1.3.0", false, true, false, false},
		{FlavorOpenSearch, "2.3.0", false, true, true, true},
		{FlavorOpenSearch, "2.4.0", true, true, true, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.flavor)+" "+tt.raw, func(t *testing.T) {
			v, err := ParseVersion(tt.flavor, tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := v.SupportsPIT(); got != tt.pit {
				t.Errorf("SupportsPIT = %v, want %v", got, tt.pit)
			}
			if got := v.SupportsSearchAfter(); got != tt.searchAfter {
				t.Errorf("SupportsSearchAfter = %v, want %v", got, tt.searchAfter)
			}
			if got := v.RemovedMappingTypes(); got != tt.removedTypes {
				t.Errorf("RemovedMappingTypes = %v, want %v", got, tt.removedTypes)
			}
			if got := v.UsesClusterManagerTerm(); got != tt.clusterManager {
				t.Errorf("UsesClusterManagerTerm = %v, want %v", got, tt.clusterManager)
			}
		})
	}
}
