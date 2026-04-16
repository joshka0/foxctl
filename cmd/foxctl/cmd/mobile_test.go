package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMobileGestureCoordinates(t *testing.T) {
	got, err := mobileGestureCoordinates("scroll-up", 100, 200)
	if err != nil {
		t.Fatalf("mobileGestureCoordinates: %v", err)
	}
	want := mobileGesture{startX: 50, startY: 144, endX: 50, endY: 56}
	if got != want {
		t.Fatalf("gesture = %+v, want %+v", got, want)
	}
}

func TestMobileCenterFromBoundsString(t *testing.T) {
	tests := []struct {
		name   string
		bounds string
		wantX  int
		wantY  int
	}{
		{
			name:   "android bounds",
			bounds: "[10,20][110,220]",
			wantX:  60,
			wantY:  120,
		},
		{
			name:   "ios frame",
			bounds: "{{10, 20}, {100, 200}}",
			wantX:  60,
			wantY:  120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, ok := mobileCenterFromBoundsString(tt.bounds)
			if !ok {
				t.Fatalf("expected center")
			}
			if x != tt.wantX || y != tt.wantY {
				t.Fatalf("center = (%d,%d), want (%d,%d)", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestFindMobileElementCenterExactSelector(t *testing.T) {
	elements := []any{
		map[string]any{
			"text":        "Cancel",
			"resource_id": "com.example:id/cancel",
			"bounds":      "[0,0][40,40]",
		},
		map[string]any{
			"text":        "Search",
			"resource_id": "com.example:id/search",
			"bounds":      "[100,200][300,260]",
		},
	}

	x, y, ok := findMobileElementCenter(mobileAndroid, elements, "com.example:id/search", "")
	if !ok {
		t.Fatalf("expected selector match")
	}
	if x != 200 || y != 230 {
		t.Fatalf("center = (%d,%d), want (200,230)", x, y)
	}

	if _, _, ok := findMobileElementCenter(mobileAndroid, elements, "search", ""); ok {
		t.Fatalf("selector should require exact field match")
	}
}

func TestFindMobileElementCenterIOSAXFields(t *testing.T) {
	elements := []any{
		map[string]any{
			"AXLabel":    "Join Praze",
			"AXUniqueId": "home-join-button",
			"frame": map[string]any{
				"x":      float64(39),
				"y":      float64(607),
				"width":  float64(86),
				"height": float64(39),
			},
		},
	}

	x, y, ok := findMobileElementCenter(mobileIOS, elements, "home-join-button", "")
	if !ok {
		t.Fatalf("expected id selector match")
	}
	if x != 82 || y != 627 {
		t.Fatalf("center = (%d,%d), want (82,627)", x, y)
	}

	if _, _, ok := findMobileElementCenter(mobileIOS, elements, "", "Join Praze"); !ok {
		t.Fatalf("expected label selector match")
	}
}

func TestMobileReadTextInputRequiresOneSource(t *testing.T) {
	if _, err := mobileReadTextInput(strings.NewReader("stdin"), []string{"arg"}, true, ""); err == nil {
		t.Fatalf("expected source conflict error")
	}
	got, err := mobileReadTextInput(strings.NewReader("stdin"), nil, true, "")
	if err != nil {
		t.Fatalf("mobileReadTextInput: %v", err)
	}
	if got != "stdin" {
		t.Fatalf("text = %q, want stdin", got)
	}
}

func TestMobileScreenshotPathUsesDirectory(t *testing.T) {
	dir := t.TempDir()
	got, err := mobileScreenshotPath(mobileIOS, dir)
	if err != nil {
		t.Fatalf("mobileScreenshotPath: %v", err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("path dir = %q, want %q", filepath.Dir(got), dir)
	}
	if !strings.HasPrefix(filepath.Base(got), "ios_screenshot_") {
		t.Fatalf("path base = %q, want ios_screenshot_ prefix", filepath.Base(got))
	}
}
