package digest

import "testing"

func TestBuildDeliveries(t *testing.T) {
	tests := []struct {
		name string
		run  DigestRun
		want []string
	}{
		{
			name: "only active opted-in subscribers receive the asset",
			run: DigestRun{
				Edition: "September drops",
				Assets:  []Asset{{Title: "Lighting presets", DownloadURL: "https://assets.example/presets"}},
				Subscribers: []Subscriber{
					{Email: "active@example.com", Active: true, DigestEnabled: true},
					{Email: "paused@example.com", Active: false, DigestEnabled: true},
					{Email: "opted-out@example.com", Active: true, DigestEnabled: false},
				},
			},
			want: []string{"active@example.com"},
		},
		{
			name: "an empty eligible audience produces no sends",
			run: DigestRun{
				Edition:     "September drops",
				Assets:      []Asset{{Title: "Lighting presets", DownloadURL: "https://assets.example/presets"}},
				Subscribers: []Subscriber{{Email: "paused@example.com", Active: false, DigestEnabled: true}},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildDeliveries(tt.run)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d deliveries, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].To != tt.want[i] {
					t.Errorf("delivery %d went to %q, want %q", i, got[i].To, tt.want[i])
				}
			}
		})
	}
}
