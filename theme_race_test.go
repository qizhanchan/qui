package qui

import (
	"sync"
	"testing"
)

// Page threads read the theme while the UI thread switches it (RFC 0005):
// under -race this reports any write to a Theme a reader can see.
func TestSetThemeWhileReading(t *testing.T) {
	prev := *CurrentTheme()
	defer SetTheme(prev)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				th := CurrentTheme()
				_ = th.Text.R + th.FontBase
				_ = ThemeFont(TextBody)
			}
		}()
	}
	other := LightTheme
	other.FontBase += 2
	other.Text = ColorWhite
	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			SetTheme(other)
		} else {
			SetTheme(LightTheme)
		}
	}
	close(stop)
	wg.Wait()
}
