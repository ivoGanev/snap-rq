package commands

import "testing"

func TestColorSlotValue(t *testing.T) {
	str := func(s string) *string { return &s }

	for _, color := range []*string{nil, str("")} {
		if got, err := ColorSlotValue(color); err != nil || got != nil {
			t.Errorf("ColorSlotValue(%v) = %v, %v; want nil, nil", color, got, err)
		}
	}
	if got, err := ColorSlotValue(str("secondary")); err != nil || got != "secondary" {
		t.Errorf("ColorSlotValue(secondary) = %v, %v", got, err)
	}
	for _, bad := range []string{"#AECBFA", "magenta", "Primary"} {
		if _, err := ColorSlotValue(str(bad)); err == nil {
			t.Errorf("ColorSlotValue(%q) accepted an invalid slot", bad)
		}
	}
}
