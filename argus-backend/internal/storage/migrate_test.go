package storage

import "testing"

func TestCheckDowngrade(t *testing.T) {
	cases := []struct {
		name    string
		current int64
		latest  int64
		wantErr bool
	}{
		{"fresh database", 0, 3, false},
		{"behind", 2, 3, false},
		{"up to date", 3, 3, false},
		{"newer than build", 4, 3, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDowngrade(tc.current, tc.latest)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkDowngrade(%d, %d) = %v, wantErr %v", tc.current, tc.latest, err, tc.wantErr)
			}
		})
	}
}
