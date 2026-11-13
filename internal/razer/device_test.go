package razer

import (
	"sync"
	"testing"
)

// The tray, the scene switcher, the binding keeper and the page query one
// device from separate goroutines; an unguarded txOK map ends the process
// with "concurrent map read and map write".
func TestRunConcurrent(t *testing.T) {
	d := &Device{Model: &Model{tx: map[string]byte{}}, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) { return r, nil }
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 20000 {
				if _, err := d.run(string(rune('a'+(g+i)%4)), 0x00, 0x81, 0x02); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}
