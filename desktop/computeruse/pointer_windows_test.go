//go:build windows

package computeruse

import "testing"

func TestAbsolutePointerCoordinatesAndMarker(t *testing.T) {
	for _, bounds := range []Rect{{0, 0, 1920, 1080}, {-1920, -180, 4480, 1620}, {0, 0, 3840, 2160}} {
		for _, point := range []struct {
			x, y   int
			dx, dy int32
		}{
			{bounds.X, bounds.Y, 0, 0},
			{bounds.X + bounds.Width - 1, bounds.Y + bounds.Height - 1, 65535, 65535},
		} {
			input, err := absolutePointerInput(point.x, point.y, bounds)
			if err != nil {
				t.Fatal(err)
			}
			if input.DX != point.dx || input.DY != point.dy {
				t.Fatalf("mapping %+v -> %+v", point, input)
			}
			if input.ExtraInfo != injectedMarker || input.Flags != mouseeventfMove|mouseeventfAbsolute|mouseeventfVirtualDesk {
				t.Fatal("unmarked or non-absolute pointer movement")
			}
		}
		if _, err := absolutePointerInput(bounds.X-1, bounds.Y, bounds); err == nil {
			t.Fatal("out-of-desktop input accepted")
		}
	}
}
